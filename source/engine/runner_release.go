package engine

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (s *Store) LatestRun(id string) (Run, error) {
	var r Run
	root, e := s.Base(id)
	if e != nil {
		return r, e
	}
	entries, e := os.ReadDir(filepath.Join(root, "runs"))
	if e != nil {
		return r, e
	}
	type rec struct{ run Run }
	all := []Run{}
	for _, d := range entries {
		if !d.IsDir() {
			continue
		}
		b, e := os.ReadFile(filepath.Join(root, "runs", d.Name(), "results.json"))
		if e != nil {
			continue
		}
		var x Run
		if json.Unmarshal(b, &x) == nil && x.ProjectID == id {
			all = append(all, x)
		}
	}
	if len(all) == 0 {
		return r, errors.New("Δεν υπάρχουν ακόμη αποτελέσματα")
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Started.After(all[j].Started) })
	return all[0], nil
}
func (s *Store) Approve(id string) (string, error) {
	p, manifest, e := s.Get(id)
	if e != nil {
		return "", e
	}
	run, e := s.LatestRun(id)
	if e != nil {
		return "", e
	}
	current, e := HashTree(filepath.Join(s.Root, "projects", id, "source"))
	if e != nil {
		return "", e
	}
	if run.SourceHash != current {
        return "",errors.New("Ο κώδικας άλλαξε μετά τα τεστ — απαιτούνται νέοι έλεγχοι")
    }
    if run.Stage==StageChecks || run.Stage==StageSetup {
        return "",errors.New("Δεν εγκρίνεται έκδοση χωρίς πραγματική κατασκευή και έλεγχο EXE")
    }
    if run.PlanHash=="" || run.PlanHash!=planDigest(manifest) {
        return "",errors.New("Το σχέδιο ελέγχων άλλαξε — απαιτούνται νέοι έλεγχοι")
    }
    if len(run.Artifacts)==0 {
        return "",errors.New("Δεν υπάρχει επαληθευμένο Windows EXE προς προώθηση")
    }
    verifiedBuild,verifiedLaunch:=false,false
    for _,result:=range run.Results {
        if result.Phase=="BUILD" && result.Required && result.Status=="PASS"{verifiedBuild=true}
        if result.Phase=="SMOKE" && result.Required && result.Status=="PASS"{verifiedLaunch=true}
    }
    if !verifiedBuild || !verifiedLaunch {
        return "",errors.New("Η κατασκευή και η πραγματική εκκίνηση του EXE είναι υποχρεωτικές")
    }
    if IsBlocked(run) {
		return "", errors.New("Αποκλείεται η σταθερή έκδοση: αποτυχία ή έλλειψη υποχρεωτικών τεστ")
	}
	root := filepath.Join(s.Root, "projects", id)
    for _,art:=range run.Artifacts {
        if !zipPathOK(art.Name) || !strings.HasSuffix(strings.ToLower(art.Name),".exe") {
            return "",errors.New("Μη ασφαλές ή μη εκτελέσιμο αρχείο προς έγκριση")
        }
        path:=filepath.Join(root,"runs",run.ID,"artifacts",filepath.FromSlash(art.Name))
        data,e:=os.ReadFile(path)
        if e!=nil{return "",errors.New("Λείπει το ελεγμένο EXE: "+e.Error())}
        digest:=sha256.Sum256(data)
        if hex.EncodeToString(digest[:])!=art.SHA256 || int64(len(data))!=art.Bytes {
            return "",errors.New("Αλλοιώθηκε το παραγόμενο EXE μετά τους ελέγχους")
        }
    }
    // Inspect the real EXE and required phases first; then enforce the
    // reviewer gate before any stable-version write.
    if run.Stage==StageBuild {
        if err:=s.ReadyForBuild(id);err!=nil{return "",err}
    }
    dst := filepath.Join(root, "stable", run.ID)
	if exists(dst) {
		return dst, nil
	}
	stage := dst + ".new"
	os.RemoveAll(stage)
	if e = copyTree(filepath.Join(root, "source"), filepath.Join(stage, "source")); e != nil {
		os.RemoveAll(stage)
		return "", e
	}
	if exists(filepath.Join(root, "runs", run.ID, "artifacts")) {
		if e = copyTree(filepath.Join(root, "runs", run.ID, "artifacts"), filepath.Join(stage, "artifacts")); e != nil {
			os.RemoveAll(stage)
			return "", e
		}
	}
	verifiedHash, verr := HashTree(filepath.Join(stage, "source"))
	if verr != nil || verifiedHash != run.SourceHash {
		os.RemoveAll(stage)
		return "", errors.New("Αποτυχία επαλήθευσης πηγών σταθερής έκδοσης")
	}
	if e = atomicJSON(filepath.Join(stage, "approval.json"), map[string]any{"project": p, "run": run}); e != nil {
		os.RemoveAll(stage)
		return "", e
	}
	if e = os.Rename(stage, dst); e != nil {
		os.RemoveAll(stage)
		return "", e
	}
	return dst, nil
}
func (s *Store) Backup(id string) (string, error) {
	p, _, e := s.Get(id)
	if e != nil {
		return "", e
	}
	src := filepath.Join(s.Root, "projects", id, "source")
	h, e := HashTree(src)
	if e != nil {
		return "", e
	}
	dst := filepath.Join(s.Root, "backups", id+"-"+newID())
	if e = copyTree(src, filepath.Join(dst, "source")); e != nil {
		os.RemoveAll(dst)
		return "", e
	}
	verify, verr := HashTree(filepath.Join(dst, "source"))
	if verr != nil || verify != h {
		os.RemoveAll(dst)
		return "", errors.New("Αποτυχία επαλήθευσης ακεραιότητας αντιγράφου")
	}
	if e = atomicJSON(filepath.Join(dst, "backup.json"), map[string]any{"name": p.Name, "version": p.Version, "language": p.Language, "hash": h, "created": time.Now()}); e != nil {
		return "", e
	}
	return dst, nil
}
func (s *Store) RestoreNewest(id string) (Project, error) {
	var result Project
	matches, e := filepath.Glob(filepath.Join(s.Root, "backups", id+"-*"))
	if e != nil {
		return result, e
	}
	if len(matches) == 0 {
		return result, errors.New("Δεν υπάρχει αντίγραφο για το έργο")
	}
	sort.Slice(matches, func(i, j int) bool {
		a, _ := os.Stat(matches[i])
		b, _ := os.Stat(matches[j])
		return a.ModTime().After(b.ModTime())
	})
	meta, e := os.ReadFile(filepath.Join(matches[0], "backup.json"))
	if e != nil {
		return result, e
	}
	var v struct{ Name, Version, Language, Hash string }
	if e = json.Unmarshal(meta, &v); e != nil {
		return result, e
	}
	got, e := HashTree(filepath.Join(matches[0], "source"))
	if e != nil {
		return result, e
	}
	if got != v.Hash {
		return result, errors.New("Το αντίγραφο ασφαλείας αλλοιώθηκε")
	}
	result = Project{ID: newID(), Name: v.Name + " (επαναφορά)", Version: v.Version, Language: v.Language, SourceHash: got, ImportedAt: time.Now()}
	dst := filepath.Join(s.Root, "projects", result.ID)
	stage := dst + ".new"
	if e = copyTree(filepath.Join(matches[0], "source"), filepath.Join(stage, "source")); e != nil {
		os.RemoveAll(stage)
		return result, e
	}
	if e = atomicJSON(filepath.Join(stage, "project.json"), result); e != nil {
		os.RemoveAll(stage)
		return result, e
	}
	if e = os.Rename(stage, dst); e != nil {
		os.RemoveAll(stage)
		return result, e
	}
	return result, nil
}
func (s *Store) ExportLatest(id string) (string, error) {
    project, _, e := s.Get(id)
	if e != nil {
		return "", e
	}
	r, e := s.LatestRun(id)
	if e != nil {
		return "", e
	}
	dir := filepath.Join(s.Root, "projects", id, "runs", r.ID)
	// Human-readable filename: stage, local date and project name, never a
    // second indistinguishable opaque Results archive.
    stageName:="ΠΑΛΑΙΑ_ΑΝΑΦΟΡΑ"
    switch r.Stage {
    case StageChecks: stageName="ΕΛΕΓΧΟΙ_STRESS"
    case StageBuild: stageName="EXE"
    case StageSetup: stageName="SETUP"
    }
    var clean strings.Builder
    for _,ch:=range project.Name {
        if clean.Len()>72 {break}
        if (ch>='a'&&ch<='z')||(ch>='A'&&ch<='Z')||(ch>='0'&&ch<='9')||(ch>='Α'&&ch<='Ω')||(ch>='α'&&ch<='ω')||ch=='_'||ch=='-' {
            clean.WriteRune(ch)
        } else if ch==' '||ch=='.' {clean.WriteByte('_')}
    }
    name:=strings.Trim(clean.String(),"_")
    if name==""{name="ΕΡΓΟ"}
    stamp:=r.Started.Local().Format("20060102_150405")
    out := filepath.Join(s.Root, "reports", "ΑΝΑΦΟΡΑ_"+stageName+"_"+name+"_"+stamp+"_"+r.ID[:8]+".zip")
	entries := map[string][]byte{}
	for _, name := range []string{"SUMMARY_GR.txt", "results.json"} {
		b, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			return "", e
		}
		entries[name] = b
	}
	env, _ := json.MarshalIndent(map[string]any{"host_os": r.HostOS, "app_version": ProductVersion, "run_id": r.ID, "source_sha256": r.SourceHash}, "", "  ")
	entries["environment.json"] = env
    // A passed CHECKS report carries only fingerprints, never a prefilled approval.
    // ChatGPT examines the entire report and issues a separate matching decision.
    if r.Stage==StageChecks && !r.Blocked {
        request, e:=s.ReviewRequest(id)
        if e==nil {
            raw, marshalErr:=json.MarshalIndent(request, "", "  ")
            if marshalErr!=nil {return "",marshalErr}
            entries["review_request.json"]=append(raw,'\n')
        }
    }
	artifacts, _ := json.MarshalIndent(r.Artifacts, "", "  ")
	entries["artifacts/manifest.json"] = artifacts
	var junit strings.Builder
	junit.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<testsuite name=\"Κέντρο Προγραμματισμού\">\n")
	for _, v := range r.Results {
		junit.WriteString("<testcase name=\"")
		xml.EscapeText(&junit, []byte(v.Phase+" - "+v.Name))
		junit.WriteString("\">")
		if v.Status == "FAIL" {
			junit.WriteString("<failure>")
			xml.EscapeText(&junit, []byte(v.Reason))
			junit.WriteString("</failure>")
		}
		if v.Status == "NOT RUN" || v.Status == "CANCELLED" {
			junit.WriteString("<skipped>")
			xml.EscapeText(&junit, []byte(v.Reason))
			junit.WriteString("</skipped>")
		}
		junit.WriteString("</testcase>\n")
	}
	junit.WriteString("</testsuite>\n")
	entries["results.junit.xml"] = []byte(junit.String())
	// Preserve the evidence needed to diagnose failed tests and timeouts.
	// Only bounded, ordinary text logs produced inside this run are exported.
	logDir := filepath.Join(dir, "logs")
	logFiles, logErr := os.ReadDir(logDir)
	if logErr != nil && !errors.Is(logErr, os.ErrNotExist) { return "", logErr }
	if len(logFiles) > 200 { return "", errors.New("Πάρα πολλά αρχεία καταγραφής στην αναφορά") }
	var totalLogBytes int64
	for _, file := range logFiles {
		if file.IsDir() || !file.Type().IsRegular() { continue }
		if !strings.HasSuffix(strings.ToLower(file.Name()), ".txt") { continue }
		if !zipPathOK(file.Name()) { continue }
		info, err := file.Info()
		if err != nil { return "", err }
		if info.Size() > 250000 || totalLogBytes + info.Size() > 16000000 {
			return "", errors.New("Υπέρβαση ασφαλούς ορίου αρχείων καταγραφής")
		}
		content, err := os.ReadFile(filepath.Join(logDir, file.Name()))
		if err != nil { return "", err }
		totalLogBytes += int64(len(content))
		entries["logs/"+file.Name()] = content
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	var sums strings.Builder
	for _, name := range names {
		h := sha256.Sum256(entries[name])
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(h[:]), name)
	}
	entries["checksums.sha256"] = []byte(sums.String())
	names = append(names, "checksums.sha256")
	f, e := os.Create(out)
	if e != nil {
		return "", e
	}
	z := zip.NewWriter(f)
	for _, name := range names {
		w, e := z.Create(name)
		if e != nil {
			z.Close()
			f.Close()
			return "", e
		}
		if _, e = w.Write(entries[name]); e != nil {
			z.Close()
			f.Close()
			return "", e
		}
	}
	if e = z.Close(); e != nil {
		f.Close()
		return "", e
	}
	if e = f.Close(); e != nil {
		return "", e
	}
	return out, nil
}
