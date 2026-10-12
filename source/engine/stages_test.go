package engine

import (
    "context"
    "encoding/json"
    "os"
    "path/filepath"
    "runtime"
    "strings"
    "testing"
)

// CI fixture demonstrates that checking cannot execute the build recipe.
// The same test is run on Linux and real Windows runners.
func stageFixture(t *testing.T, withSetup bool) (*Store,string) {
    t.Helper()
    s:=newTestStore(t)
    manifest:=Manifest{
        Name:"Phased demo",Version:"1.0",Language:"Go",
        Tests:[]Step{{Name:"Unit test",Command:"go",Args:[]string{"test","-run","TestReady","./..."},TimeoutSeconds:90}},
        Stress:[]StressSpec{{Name:"Concurrent check",Kind:"parallel",Command:"go",Args:[]string{"test","-run","TestReady","./..."},Repeats:2,Parallel:2,TimeoutSeconds:90}},
        Build:&Step{Name:"Build real EXE",Command:"go",Args:[]string{"build","-o","app.exe","."},TimeoutSeconds:90},
        Smoke:&Step{Name:"Start EXE",Command:"./app.exe",ExpectContains:"WINDOW_STAYS_OPEN"},
    }
    if withSetup {manifest.Setup=&Step{Name:"Build optional installer",Command:"go",Args:[]string{"build","-o","setup-installer.exe","./setup"},TimeoutSeconds:90}}
    b,e:=json.Marshal(manifest)
    if e!=nil{t.Fatal(e)}
    files:=map[string]string{
        "go.mod":"module example.local/phases\n\ngo 1.23\n",
        "main.go":"package main\nimport \"time\"\nfunc main(){time.Sleep(20*time.Second)}\n",
        "main_test.go":"package main\nimport \"testing\"\nfunc TestReady(t *testing.T) {}\n",
        "project-manifest.json":string(b),
    }
    if withSetup{files["setup/main.go"]="package main\nfunc main(){}\n"}
    p,e:=s.ImportZIP(zipFixture(t,files))
    if e!=nil{t.Fatal(e)}
    return s,p.ID
}

func TestChecksNeverBuildOrCreateExe(t *testing.T){
    s,id:=stageFixture(t,false)
    if err:=s.ReadyForBuild(id);err==nil{t.Fatal("build authorized before any checks")}
    if err:=s.ReadyForSetup(id);err==nil{t.Fatal("setup authorized before checks and build")}
    r,err:=s.RunChecks(context.Background(),id,nil)
    if err!=nil{t.Fatal(err)}
    if r.Stage!=StageChecks{t.Fatalf("wrong preflight stage: %q",r.Stage)}
    if len(r.Artifacts)!=0{t.Fatal("unit/stress unexpectedly produced EXE",r.Artifacts)}
    for _,result:=range r.Results {if result.Phase=="BUILD"||result.Phase=="SMOKE"||result.Phase=="SETUP"||result.Phase=="VISUAL"{t.Fatal("build work executed during preflight",result)}}
    base,_:=s.Base(id)
    if _,err:=os.Stat(filepath.Join(base,"runs",r.ID,"artifacts"));err==nil {t.Fatal("preflight generated artifacts")}
    if _,err:=os.Stat(filepath.Join(base,"runs",r.ID,"work","app.exe"));err==nil {t.Fatal("preflight executed build command")}
    path,e:=s.ExportLatest(id)
    if e!=nil{t.Fatal(e)}
    if _,e=os.Stat(path);e!=nil{t.Fatal("checks report not saved",e)}
    if runtime.GOOS=="windows" {
        if r.Blocked{t.Fatalf("preflight should pass on real Windows: %+v",r.Results)}
        if err=s.ReadyForBuild(id);err!=nil{t.Fatal(err)}
        if err=s.ReadyForSetup(id);err==nil{t.Fatal("setup authorized before build")}
    } else if !r.Blocked {t.Fatal("non-Windows report authorized Windows release")}
}

func TestBuildGateRejectsChangedSourcesAndPlan(t *testing.T){
    if runtime.GOOS!="windows"{t.Skip("Real Windows preflight gate")}
    s,id:=stageFixture(t,false)
    r,e:=s.RunChecks(context.Background(),id,nil)
    if e!=nil||r.Blocked{t.Fatalf("preflight %v %+v",e,r.Results)}
    base,_:=s.Base(id)
    if e=os.WriteFile(filepath.Join(base,"source","main.go"),[]byte("package main\nfunc main(){}\n"),0600);e!=nil{t.Fatal(e)}
    if err:=s.ReadyForBuild(id);err==nil||!strings.Contains(err.Error(),"άλλαξε"){t.Fatalf("changed source accepted: %v",err)}
    if _,e=s.BuildChecked(context.Background(),id,nil);e==nil{t.Fatal("built changed source")}
}

func TestEndToEndStagesAreSeparateOnWindows(t *testing.T) {
    if runtime.GOOS!="windows" || os.Getenv("KENTRO_RUN_PY_BUILD_TEST")!="1" {
        t.Skip("Real Windows full integration only")
    }
    s,id:=stageFixture(t,true)
    if _,e:=s.BuildChecked(context.Background(),id,nil);e==nil{t.Fatal("build skipped preflight gate")}
    if _,e:=s.BuildSetup(context.Background(),id,nil);e==nil{t.Fatal("setup skipped EXE gate")}
    checks,e:=s.RunChecks(context.Background(),id,nil)
    if e!=nil||checks.Blocked{t.Fatalf("checks failed: %v %+v",e,checks.Results)}
    if _,e=s.BuildSetup(context.Background(),id,nil);e==nil{t.Fatal("setup skipped completed EXE gate")}
    built,e:=s.BuildChecked(context.Background(),id,nil)
    if e!=nil||built.Blocked{t.Fatalf("build/smoke failed %v %+v",e,built.Results)}
    if built.Stage!=StageBuild||len(built.Artifacts)<1{t.Fatal("no verified EXE after build")}
    foundSmoke:=false
    for _,v:=range built.Results {if v.Phase=="SMOKE"&&v.Status=="PASS"{foundSmoke=true}}
    if !foundSmoke{t.Fatal("built EXE never started")}
    stable,e:=s.Approve(id)
    if e!=nil{t.Fatal(e)}
    if _,e=os.Stat(filepath.Join(stable,"artifacts","app.exe"));e!=nil{t.Fatal("no promoted exe",e)}
    setup,e:=s.BuildSetup(context.Background(),id,nil)
    if e!=nil||setup.Blocked{t.Fatalf("optional setup failed: %v %+v",e,setup.Results)}
    if setup.Stage!=StageSetup||len(setup.Artifacts)<1{t.Fatal("setup not separate and verified")}
    base,_:=s.Base(id)
    if _,e=os.Stat(filepath.Join(base,"runs",setup.ID,"artifacts","setup-installer.exe"));e!=nil{t.Fatal("setup artifact missing",e)}
    if _,e=os.Stat(filepath.Join(stable,"artifacts","app.exe"));e!=nil{t.Fatal("setup changed approved exe",e)}
}

func TestFailedChecksCannotAuthorizeExe(t *testing.T) {
    s:=newTestStore(t)
    m:=Manifest{Name:"Fail case",Tests:[]Step{{Name:"Broken test",Command:"go",Args:[]string{"test","./..."}}},Stress:[]StressSpec{{Name:"Load",Kind:"parallel",Command:"go",Args:[]string{"test","./..."},Repeats:1,Parallel:1}},Build:&Step{Command:"go",Args:[]string{"build","-o","app.exe","."}},Smoke:&Step{Command:"./app.exe",ExpectContains:"WINDOW_STAYS_OPEN"}}
    b,_:=json.Marshal(m)
    p,e:=s.ImportZIP(zipFixture(t,map[string]string{
        "go.mod":"module example.local/bad\n\ngo 1.23\n",
        "main.go":"package main\nfunc main(){}\n",
        "main_test.go":"package main\nimport \"testing\"\nfunc TestFail(t *testing.T){t.Fatal(\"broken\")}\n",
        "project-manifest.json":string(b),
    }))
    if e!=nil{t.Fatal(e)}
    r,e:=s.RunChecks(context.Background(),p.ID,nil)
    if e!=nil{t.Fatal(e)}
    if !r.Blocked{t.Fatal("failed tests marked good")}
    if err:=s.ReadyForBuild(p.ID);err==nil{t.Fatal("failed tests unlocked EXE")}
}
