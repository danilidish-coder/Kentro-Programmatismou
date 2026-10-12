package engine

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

func runStress(ctx context.Context, work, logs string, i int, sp StressSpec) Result {
	r := Result{Name: sp.Name, Phase: "STRESS", Required: sp.Mandatory(), ExitCode: -1}
	if r.Name == "" {
		r.Name = "Σενάριο πίεσης"
	}
	if ctx.Err() != nil {
		r.Status = "CANCELLED"
		return r
	}
	if sp.Kind == "restart" {
		if sp.Health == nil {
			r.Status = "NOT RUN"
			r.Reason = "Απουσιάζει υποχρεωτικός έλεγχος υγείας μετά την επανεκκίνηση"
			return r
		}
		return restartStress(ctx, work, logs, i, sp)
	}
	// A missing compiler means NOT RUN, not a failed stress scenario.
	// Resolve once before starting parallel workers; never count setup errors as failures.
	if _, _, err := resolve(work, sp.Command, sp.Args); err != nil {
		r.Status = "NOT RUN"
		r.Reason = err.Error()
		return r
	}
	count := sp.Repeats
	if count <= 0 {
		count = 3
	}
	parallel := sp.Parallel
	if parallel <= 0 {
		parallel = 2
	}
	start := time.Now()
	jobs := make(chan int)
	errs := make(chan string, count)
	var wg sync.WaitGroup
	for w := 0; w < parallel; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					errs <- "Ακυρώθηκε"
					continue
				}
				st := runStep(ctx, work, logs, "STRESS", fmt.Sprintf("stress-%d-%03d", i, j), Step{Name: sp.Name, Command: sp.Command, Args: sp.Args, TimeoutSeconds: sp.TimeoutSeconds})
				if st.Status != "PASS" {
					errs <- st.Status + ": " + st.Reason
				} else {
					errs <- ""
				}
			}
		}()
	}
	for j := 0; j < count; j++ {
		jobs <- j
	}
	close(jobs)
	wg.Wait()
	close(errs)
	bad := 0
	reason := ""
	for s := range errs {
		if s != "" {
			bad++
			if reason == "" {
				reason = s
			}
		}
	}
	r.DurationMS = time.Since(start).Milliseconds()
	if bad > 0 {
		r.Status = "FAIL"
		r.Reason = fmt.Sprintf("%d/%d αποτυχίες: %s", bad, count, reason)
	} else {
		r.Status = "PASS"
		r.ExitCode = 0
		r.Reason = fmt.Sprintf("%d επαναλήψεις, έως %d παράλληλες εκτελέσεις", count, parallel)
	}
	return r
}
func restartStress(ctx context.Context, work, logs string, i int, sp StressSpec) Result {
	r := Result{Name: sp.Name, Phase: "STRESS", Required: sp.Mandatory(), ExitCode: -1}
	reps := sp.Repeats
	if reps < 1 {
		reps = 2
	}
	start := time.Now()
	exe, args, e := resolve(work, sp.Command, sp.Args)
	if e != nil {
		r.Status = "NOT RUN"
		r.Reason = e.Error()
		return r
	}
	for k := 0; k < reps; k++ {
		if ctx.Err() != nil {
			r.Status = "CANCELLED"
			return r
		}
		cmd := exec.Command(exe, args...)
		hideToolConsole(cmd)
		cmd.Dir = work
		cmd.Env = offlineEnv()
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if e := cmd.Start(); e != nil {
			r.Status = "FAIL"
			r.Reason = e.Error()
			return r
		}
		time.Sleep(250 * time.Millisecond)
		cmd.Process.Kill()
		cmd.Wait()
		st := runStep(ctx, work, logs, "STRESS", fmt.Sprintf("restart-%d-%d", i, k), *sp.Health)
		if st.Status != "PASS" {
			r.Status = "FAIL"
			r.Reason = "Αποτυχία ανάκαμψης μετά τον τερματισμό: " + st.Status + " " + st.Reason
			return r
		}
	}
	r.Status = "PASS"
	r.ExitCode = 0
	r.DurationMS = time.Since(start).Milliseconds()
	r.Reason = fmt.Sprintf("%d αναγκαστικοί τερματισμοί και επιτυχή health checks", reps)
	return r
}
