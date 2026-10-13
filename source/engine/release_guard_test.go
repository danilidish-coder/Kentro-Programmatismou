package engine

import (
    "context"
    "os"
    "path/filepath"
    "runtime"
    "strings"
    "testing"
)

// A green report is NOT permission to publish an EXE.
func TestSuccessfulChecksCannotBePromotedAsBinary(t *testing.T) {
    s,id:=stageFixture(t,false)
    r,e:=s.RunChecks(context.Background(),id,nil)
    if e!=nil {t.Fatal(e)}
    if runtime.GOOS=="windows" && r.Blocked {t.Fatalf("preflight failed: %+v",r.Results)}
    if _,e=s.Approve(id);e==nil || (!strings.Contains(e.Error(),"EXE")&&!strings.Contains(e.Error(),"έλεγχ")){
        t.Fatalf("unsafe approval of checks-only report: %v",e)
    }
}

func TestNoArtifactCanBePromoted(t *testing.T) {
    s,id:=stageFixture(t,false)
    r,e:=s.RunChecks(context.Background(),id,nil)
    if e!=nil{t.Fatal(e)}
    r.Stage=StageBuild
    r.Blocked=false
    r.Results=append(r.Results,Result{Phase:"BUILD",Status:"PASS",Required:true},
        Result{Phase:"SMOKE",Status:"PASS",Required:true})
    r.Started=r.Started.Add(60*1e9)
    if e=s.saveStage(&r,Project{Name:"test"},mustBase(t,s,id));e!=nil{t.Fatal(e)}
    if _,e=s.Approve(id);e==nil || !strings.Contains(e.Error(),"EXE"){
        t.Fatalf("approved empty artifact manifest: %v",e)
    }
}
func mustBase(t *testing.T,s *Store,id string) string {
    t.Helper()
    p,e:=s.Base(id)
    if e!=nil{t.Fatal(e)}
    return p
}

func TestTamperedExeNeverPromoted(t *testing.T) {
    if runtime.GOOS!="windows" || os.Getenv("KENTRO_RUN_PY_BUILD_TEST")!="1"{
        t.Skip("Full real Windows EXE integration test")
    }
    s,id:=stageFixture(t,false)
    checks,e:=s.RunChecks(context.Background(),id,nil)
    if e!=nil||checks.Blocked{t.Fatalf("checks failed %v %+v",e,checks.Results)}
    approveSyntheticReview(t,s,id)
    build,e:=s.BuildChecked(context.Background(),id,nil)
    if e!=nil||build.Blocked{t.Fatalf("build failed: %v %+v",e,build.Results)}
    dir:=filepath.Join(mustBase(t,s,id),"runs",build.ID,"artifacts")
    if len(build.Artifacts)==0{t.Fatal("missing real EXE")}
    exe:=filepath.Join(dir,filepath.FromSlash(build.Artifacts[0].Name))
    f,e:=os.OpenFile(exe,os.O_APPEND|os.O_WRONLY,0600)
    if e!=nil{t.Fatal(e)}
    if _,e=f.Write([]byte("tampered"));e!=nil{t.Fatal(e)}
    f.Close()
    if _,e=s.Approve(id);e==nil||!strings.Contains(e.Error(),"Αλλοιώθηκε"){
        t.Fatalf("damaged binary was allowed: %v",e)
    }
}
