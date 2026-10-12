package engine

import "fmt"

// DeepStressPlan raises the minimum exercise count without modifying the
// user's original manifest. This is a baseline, not a claim of exhaustive
// coverage of every realistic concurrency or infrastructure failure.
func DeepStressPlan(in StressSpec) StressSpec {
    out:=in
    if out.Kind=="restart" {
        if out.Repeats<4 {out.Repeats=4}
    } else {
        if out.Repeats<6 {out.Repeats=6}
        if out.Parallel<3 {out.Parallel=3}
    }
    return out
}

func StressPlanDescription(sp StressSpec) string {
    if sp.Kind=="restart" {
        return fmt.Sprintf("%s: %d κύκλοι αναγκαστικού τερματισμού και ανάκαμψης",sp.Name,sp.Repeats)
    }
    return fmt.Sprintf("%s: %d επαναλήψεις με έως %d παράλληλες εργασίες",sp.Name,sp.Repeats,sp.Parallel)
}
