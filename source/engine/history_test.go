package engine

import (
 "archive/zip"
 "context"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestHistoryIsScopedOrderedAndBounded(t *testing.T) {
 s:=newTestStore(t)
 p,e:=s.ImportZIP(zipFixture(t,map[string]string{"readme.txt":"hi"}))
 if e!=nil{t.Fatal(e)}
 base,_:=s.Base(p.ID)
 for i:=0;i<4;i++ {
  id:=newID()
  projectID:=p.ID
  if i==3{projectID="not-allowed"}
  run:=Run{ID:id,ProjectID:projectID,Stage:StageChecks,
   Started:time.Date(2026,10,13,12,i,0,0,time.UTC),Blocked:i==0,
   Results:[]Result{{Name:"stress",Phase:"STRESS",Status:"FAIL",Required:true,
     Reason:"Υπέρβαση χρονικού ορίου",FailureKind:"TIMEOUT"}}}
  if e:=atomicJSON(filepath.Join(base,"runs",id,"results.json"),run);e!=nil{t.Fatal(e)}
 }
 got,e:=s.History(p.ID,2)
 if e!=nil{t.Fatal(e)}
 if len(got)!=2||got[0].Run.Started.Before(got[1].Run.Started){
  t.Fatalf("history ordering/limit/isolation: %+v",got)
 }
 if len(got[0].Issues)!=1||got[0].Issues[0].Category!="TIMEOUT"{
  t.Fatalf("diagnostic category not preserved: %+v",got)
 }
 for _,v:=range got{if v.Run.ProjectID!=p.ID{t.Fatal("cross-project history leak")}}
}

func TestFailureDiagnosisSeparatesTimeoutAndCancellation(t *testing.T){
 for _,tc:=range []struct{result Result;category string}{
  {Result{Status:"FAIL",FailureKind:"TIMEOUT"},"TIMEOUT"},
  {Result{Status:"CANCELLED"},"CANCELLED"},
  {Result{Status:"FAIL",Reason:"Υπέρβαση χρονικού ορίου ή ακύρωση"},"TIMEOUT"},
  {Result{Status:"NOT RUN"},"NOT_RUN"},
  {Result{Status:"FAIL",Phase:"INTEGRITY"},"INTEGRITY"},
  {Result{Status:"FAIL",Reason:"exit 1"},"TEST_FAILURE"},
 } {
  category,_:=resultCategory(tc.result)
  if category!=tc.category{t.Fatalf("got %s, want %s",category,tc.category)}
 }
}

func TestReportContainsBoundedRunLogs(t *testing.T){
 s:=newTestStore(t)
 p,e:=s.ImportZIP(zipFixture(t,map[string]string{"README.md":"file"}))
 if e!=nil{t.Fatal(e)}
 base,_:=s.Base(p.ID)
 id:=newID()
 now:=time.Now()
 run:=Run{ID:id,ProjectID:p.ID,Stage:StageChecks,HostOS:"windows",
  Started:now,Finished:now,Blocked:true,
  Results:[]Result{{Name:"stress",Phase:"STRESS",Status:"FAIL",Required:true,
   Reason:"Υπέρβαση χρονικού ορίου",LogFile:"stress-0-000.txt"}}}
 runDir:=filepath.Join(base,"runs",id)
 if e:=atomicJSON(filepath.Join(runDir,"results.json"),run);e!=nil{t.Fatal(e)}
 if e:=atomicWrite(filepath.Join(runDir,"SUMMARY_GR.txt"),[]byte("failed"));e!=nil{t.Fatal(e)}
 if e:=atomicWrite(filepath.Join(runDir,"logs","stress-0-000.txt"),[]byte("example test evidence"));e!=nil{t.Fatal(e)}
 report,e:=s.ExportLatest(p.ID)
 if e!=nil{t.Fatal(e)}
 z,e:=zip.OpenReader(report)
 if e!=nil{t.Fatal(e)}
 defer z.Close()
 found:=false
 for _,f:=range z.File{
  if f.Name=="logs/stress-0-000.txt"{found=true}
 }
 if !found{t.Fatal("Important diagnostic logs omitted from exported report")}
 if !strings.HasSuffix(report,".zip"){t.Fatal("missing report archive extension")}
}

func TestRunStepTimeoutAndCancelAreDistinct(t *testing.T){
 if _,e:=exec.LookPath("go");e!=nil{t.Skip("Go compiler not installed")}
 work:=t.TempDir()
 logs:=t.TempDir()
 if e:=os.WriteFile(filepath.Join(work,"slow.go"),[]byte(
   "package main\nimport \"time\"\nfunc main(){time.Sleep(3*time.Second)}\n"),0600);e!=nil{t.Fatal(e)}
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 cancelled:=runStep(ctx,work,logs,"TEST","cancel",Step{
  Name:"Cancelled",Command:"go",Args:[]string{"version"},TimeoutSeconds:5})
 if cancelled.Status!="CANCELLED"||cancelled.FailureKind!="CANCELLED"{
  t.Fatalf("Cancelled reported incorrectly: %+v",cancelled)
 }
 timed:=runStep(context.Background(),work,logs,"TEST","timed",Step{
  Name:"Timeout",Command:"go",Args:[]string{"run","slow.go"},TimeoutSeconds:1})
 if timed.Status!="FAIL"||timed.FailureKind!="TIMEOUT"{
  t.Fatalf("Timeout reported incorrectly: %+v",timed)
 }
}
