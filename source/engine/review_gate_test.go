package engine

import (
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

// CI-only review receipts simulate a human decision on synthetic test data.
// Production never automatically generates decisions on passing checks.
func approveSyntheticReview(t *testing.T,s *Store,id string) {
 t.Helper()
 req,err:=s.ReviewRequest(id)
 if err!=nil{t.Fatal(err)}
 ticket:=ReviewTicket{
  Schema:req.Schema,Decision:reviewDecision,ProjectID:req.ProjectID,
  RunID:req.RunID,SourceSHA256:req.SourceSHA256,PlanSHA256:req.PlanSHA256,
  ResultsSHA256:req.ResultsSHA256,Reviewer:"ChatGPT",
  Note:"CI synthetic fixture reviewed against its complete results",
 }
 file:=filepath.Join(t.TempDir(),"synthetic-review.json")
 body,err:=json.Marshal(ticket)
 if err!=nil{t.Fatal(err)}
 if err=os.WriteFile(file,body,0600);err!=nil{t.Fatal(err)}
 status,err:=s.ImportReview(id,file)
 if err!=nil||status.Status!="APPROVED"{t.Fatalf("Synthetic review import: %v, %+v",err,status)}
}

func syntheticPassedChecks(t *testing.T,s *Store,id string,stamp time.Time)Run{
 t.Helper()
 p,m,err:=s.Get(id)
 if err!=nil{t.Fatal(err)}
 base,err:=s.Base(id)
 if err!=nil{t.Fatal(err)}
 h,err:=HashTree(filepath.Join(base,"source"))
 if err!=nil{t.Fatal(err)}
 checks:=Run{
  ID:newID(),Stage:StageChecks,ProjectID:id,
  SourceHash:h,PlanHash:planDigest(m),HostOS:"windows",
  Started:stamp,Results:[]Result{
   {Name:"Unit fixture",Phase:"TEST",Required:true,Status:"PASS"},
   {Name:"Stress fixture",Phase:"STRESS",Required:true,Status:"PASS"},
   {Name:"Integrity fixture",Phase:"INTEGRITY",Required:true,Status:"PASS"},
  },
 }
 if err=s.saveStage(&checks,p,base);err!=nil{t.Fatal(err)}
 return checks
}

func writeTicket(t *testing.T,req ReviewTicket)string{
 t.Helper()
 file:=filepath.Join(t.TempDir(),"review.json")
 body,err:=json.Marshal(req)
 if err!=nil{t.Fatal(err)}
 if err=os.WriteFile(file,body,0600);err!=nil{t.Fatal(err)}
 return file
}

func TestManualReviewRequiredAndBoundToExactRun(t *testing.T){
 s,id:=stageFixture(t,false)
 first:=syntheticPassedChecks(t,s,id,time.Now().Add(-time.Hour))
 if err:=s.ReadyForBuild(id);err==nil || !strings.Contains(err.Error(),"έγκρισης"){
  t.Fatalf("Succeeded tests must not auto-authorize build: %v",err)
 }
 if status:=s.GetReviewStatus(id);status.Status!="WAITING_REVIEW"{
  t.Fatalf("Expected WAITING_REVIEW, got %+v",status)
 }
 approveSyntheticReview(t,s,id)
 if err:=s.ReadyForBuild(id);err!=nil{t.Fatalf("Exact review should authorize build: %v",err)}
 if status:=s.GetReviewStatus(id);status.Status!="APPROVED"{t.Fatal(status)}
 later:=syntheticPassedChecks(t,s,id,time.Now().Add(time.Hour))
 if first.ID==later.ID{t.Fatal("Fixture failed to create independent run")}
 if err:=s.ReadyForBuild(id);err==nil{
  t.Fatal("Old approval authorized newer checks run")
 }
 if status:=s.GetReviewStatus(id);status.Status!="WAITING_REVIEW"{t.Fatal(status)}
 approveSyntheticReview(t,s,id)
 if err:=s.ReadyForBuild(id);err!=nil{t.Fatalf("New review should authorize exact new run: %v",err)}
}

func TestForgedMismatchedAndFailedReviewRejected(t *testing.T){
 s,id:=stageFixture(t,false)
 syntheticPassedChecks(t,s,id,time.Now())
 expected,err:=s.ReviewRequest(id)
 if err!=nil{t.Fatal(err)}
 expectedTicket:=ReviewTicket{
  Schema:expected.Schema,Decision:reviewDecision,ProjectID:expected.ProjectID,
  RunID:expected.RunID,SourceSHA256:expected.SourceSHA256,
  PlanSHA256:expected.PlanSHA256,ResultsSHA256:expected.ResultsSHA256,
  Reviewer:"ChatGPT",Note:"This test has been explicitly reviewed by a human",
 }
 invalid:=[]struct{
  name string
  mutate func(*ReviewTicket)
 }{
  {"source digest",func(x *ReviewTicket){x.SourceSHA256=strings.Repeat("a",64)}},
  {"run digest",func(x *ReviewTicket){x.ResultsSHA256=strings.Repeat("b",64)}},
  {"run identifier",func(x *ReviewTicket){x.RunID="000000000000000000000000"}},
  {"plan digest",func(x *ReviewTicket){x.PlanSHA256=strings.Repeat("c",64)}},
  {"decision denied",func(x *ReviewTicket){x.Decision="REJECTED"}},
  {"no review note",func(x *ReviewTicket){x.Note=""}},
 }
 for _,tc:=range invalid {
  t.Run(tc.name,func(t *testing.T){
   got:=expectedTicket
   tc.mutate(&got)
   file:=writeTicket(t,got)
   if _,err:=s.ImportReview(id,file);err==nil{t.Fatal("Invalid review accepted")}
   if err:=s.ReadyForBuild(id);err==nil{t.Fatal("Build unlocked despite rejected review")}
  })
 }
}

func TestReviewInvalidatedBySourceChange(t *testing.T){
 s,id:=stageFixture(t,false)
 syntheticPassedChecks(t,s,id,time.Now())
 approveSyntheticReview(t,s,id)
 base,_:=s.Base(id)
 if err:=os.WriteFile(filepath.Join(base,"source","main.go"),[]byte("package main\nfunc main(){}\n"),0600);err!=nil{t.Fatal(err)}
 if err:=s.ReadyForBuild(id);err==nil || !strings.Contains(err.Error(),"άλλαξε"){
  t.Fatalf("Modified source retained obsolete review: %v",err)
 }
}

func TestReviewRejectedWhenChecksFail(t *testing.T){
 s,id:=stageFixture(t,false)
 passed:=syntheticPassedChecks(t,s,id,time.Now())
 base,_:=s.Base(id)
 passed.Results[0].Status="FAIL"
 passed.Blocked=true
 p,_,_:=s.Get(id)
 if err:=s.saveStage(&passed,p,base);err!=nil{t.Fatal(err)}
 if _,err:=s.ReviewRequest(id);err==nil{t.Fatal("Review request created for failed checks")}
 if err:=s.ReadyForBuild(id);err==nil{t.Fatal("Build unlocked for failed checks")}
}

func TestInferredPlanCannotReceiveExternalReview(t *testing.T){
 s:=newTestStore(t)
 p,err:=s.ImportZIP(zipFixture(t,map[string]string{
  "go.mod":"module local.example/no-manifest\n\ngo 1.23\n",
  "main.go":"package main\nfunc main(){}\n",
 }))
 if err!=nil{t.Fatal(err)}
 syntheticPassedChecks(t,s,p.ID,time.Now())
 if _,err:=s.ReviewRequest(p.ID);err==nil ||
  !strings.Contains(err.Error(),"project-manifest.json"){
  t.Fatalf("Automatically inferred plan became externally approvable: %v",err)
 }
 if err:=s.ReadyForBuild(p.ID);err==nil{
  t.Fatal("Auto-inferred plan bypassed manual review")
 }
}
