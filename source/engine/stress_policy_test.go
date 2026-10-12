package engine

import (
    "context"
    "path/filepath"
    "strings"
    "testing"
)

func TestDeepStressPlanPreservesSourceAndEnforcesMeaningfulRuns(t *testing.T) {
    source:=StressSpec{Name:"parallel",Kind:"parallel",Repeats:1,Parallel:1}
    result:=DeepStressPlan(source)
    if result.Repeats<6 || result.Parallel<3 {t.Fatalf("shallow stress survived: %+v",result)}
    if source.Repeats!=1 || source.Parallel!=1 {t.Fatal("original project manifest was mutated")}
    if !strings.Contains(StressPlanDescription(result),"6 επαναλήψεις") {t.Fatal("strength not visible to user")}
    original:=StressSpec{Name:"strong",Kind:"parallel",Repeats:30,Parallel:15}
    if got:=DeepStressPlan(original);got.Repeats!=30||got.Parallel!=15 {t.Fatal("higher user-requested intensity lowered")}
    restart:=DeepStressPlan(StressSpec{Name:"reboot",Kind:"restart",Repeats:1,Parallel:1})
    if restart.Repeats<4 || restart.Parallel!=1 {t.Fatal("restart plan mutated the wrong fields")}
}

func TestDeepPreflightReportHasHumanReadableStageAndRealCounts(t *testing.T) {
    s,id:=stageFixture(t,false)
    run,err:=s.RunChecks(context.Background(),id,nil)
    if err!=nil {t.Fatal(err)}
    seen:=false
    for _,res:=range run.Results {
        if res.Phase=="STRESS" && res.Status=="PASS" {
            if !strings.Contains(res.Reason,"6 επαναλήψεις")||!strings.Contains(res.Reason,"3 παράλληλες") {
                t.Fatalf("wrong reported workload: %+v",res)
            }
            seen=true
        }
    }
    if !seen {t.Fatal("no successful real deep stress result")}
    name,err:=s.ExportLatest(id)
    if err!=nil {t.Fatal(err)}
    base:=filepath.Base(name)
    if !strings.HasPrefix(base,"ΑΝΑΦΟΡΑ_ΕΛΕΓΧΟΙ_STRESS_") || !strings.HasSuffix(base,".zip") {
        t.Fatalf("incomprehensible report filename: %s",base)
    }
}
