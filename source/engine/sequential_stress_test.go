package engine

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestSequentialStressRunsAllIterationsWithoutForcedTermination(t *testing.T) {
 work:=t.TempDir()
 logs:=t.TempDir()
 scenario:=DeepStressPlan(StressSpec{
  Name:"complete-stress-test",Kind:"sequential",Command:"go",
  Args:[]string{"version"},Parallel:1,Repeats:4,TimeoutSeconds:180,
 })
 result:=runStress(context.Background(),work,logs,0,scenario)
 if result.Status!="PASS" {
  t.Fatalf("Sequential script should finish completely: %+v",result)
 }
 if !strings.Contains(result.Reason,"4 πλήρεις διαδοχικές εκτελέσεις") {
  t.Fatalf("Stress test did not run with sequential semantics: %+v",result)
 }
 files,err:=os.ReadDir(logs)
 if err!=nil{t.Fatal(err)}
 if len(files)!=4 {t.Fatalf("Expected exactly 4 actual script executions, got %d",len(files))}
 for _,f:=range files {
  if !strings.HasPrefix(f.Name(),"stress-0-") || filepath.Ext(f.Name())!=".txt" {
   t.Fatalf("Unexpected log naming: %s",f.Name())
  }
  data,e:=os.ReadFile(filepath.Join(logs,f.Name()))
  if e!=nil{t.Fatal(e)}
  if !strings.Contains(string(data),"go version"){
   t.Fatalf("Missing completed subprocess evidence in %s",f.Name())
  }
 }
}

func TestSequentialManifestValidationAndExistingStressProtections(t *testing.T) {
 base:=Manifest{
  Name:"versioned tests",
  Tests:[]Step{{Name:"normal",Command:"go",Args:[]string{"version"},TimeoutSeconds:30}},
  Stress:[]StressSpec{{
   Name:"heavy stress",Kind:"sequential",Command:"go",
   Args:[]string{"version"},Parallel:1,Repeats:4,TimeoutSeconds:300,
  }},
  Build:&Step{Name:"binary",Command:"go",Args:[]string{"version"}},
  Smoke:&Step{Name:"smoke",Command:"go",Args:[]string{"version"}},
 }
 if err:=ValidateManifest(base);err!=nil{t.Fatalf("Proper heavy sequential plan rejected: %v",err)}
 cases:=[]struct{name string;change func(*Manifest)}{
  {"bad parallel timeout",func(m *Manifest){m.Stress[0].Kind="parallel";m.Stress[0].TimeoutSeconds=300}},
  {"bad sequential parallelism",func(m *Manifest){m.Stress[0].Parallel=3}},
  {"too long sequential",func(m *Manifest){m.Stress[0].TimeoutSeconds=901}},
  {"restart must have recovery check",func(m *Manifest){m.Stress[0].Kind="restart"}},
  {"unrecognized type",func(m *Manifest){m.Stress[0].Kind="no-check"}},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   m:=base
   m.Stress=append([]StressSpec(nil),base.Stress...)
   tc.change(&m)
   if err:=ValidateManifest(m);err==nil{t.Fatalf("Unsafe plan admitted: %+v",m.Stress[0])}
  })
 }
}

func TestSequentialStressRespectCancellation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 result:=runStress(ctx,t.TempDir(),t.TempDir(),1,
  DeepStressPlan(StressSpec{Name:"cancel",Kind:"sequential",Command:"go",Args:[]string{"version"},Parallel:1,Repeats:4}))
 if result.Status!="CANCELLED"{t.Fatalf("Cancellation not respected: %+v",result)}
}
