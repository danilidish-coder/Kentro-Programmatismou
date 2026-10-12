package engine

import (
    "context"
    "crypto/sha256"
    "debug/pe"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "runtime"
    "strings"
    "time"
)

// Stage is the persisted phase of a run; older full-pipeline runs retain the
// empty value, and cannot silently authorize new separate builds.
const (
    StageChecks = "CHECKS"
    StageBuild = "BUILD"
    StageSetup = "SETUP"
)

// IsChecksBlocked is intentionally distinct from final EXE verification.
// A passed unit/stress report authorizes a build, never a final release.
func IsChecksBlocked(r Run) bool {
    if r.Stage != StageChecks || r.HostOS != "windows" {
        return true
    }
    required := map[string]bool{"TEST":false,"STRESS":false,"INTEGRITY":false}
    for _, item := range r.Results {
        if item.Required {
            if item.Status != "PASS" {return true}
            if _, ok:=required[item.Phase];ok {required[item.Phase]=true}
        }
    }
    for _, passed:=range required {if !passed{return true}}
    return false
}

func planDigest(m Manifest) string {
    raw,_:=json.Marshal(m)
    sum:=sha256.Sum256(raw)
    return hex.EncodeToString(sum[:])
}

func (s *Store) stagedRun(id, stage string) (Run, Project, Manifest, string, string, error) {
    p,m,err:=s.Get(id)
    if err!=nil{return Run{},p,m,"","",err}
    base,err:=s.Base(id)
    if err!=nil{return Run{},p,m,"","",err}
    hash,err:=HashTree(filepath.Join(base,"source"))
    if err!=nil{return Run{},p,m,"","",err}
    r:=Run{ID:newID(),Stage:stage,ProjectID:id,SourceHash:hash,PlanHash:planDigest(m),HostOS:runtime.GOOS,Started:time.Now()}
    return r,p,m,base,hash,nil
}

func (s *Store) saveStage(r *Run,p Project,base string) error {
    r.Finished=time.Now()
    dir:=filepath.Join(base,"runs",r.ID)
    if err:=atomicJSON(filepath.Join(dir,"results.json"),r);err!=nil{return err}
    return atomicWrite(filepath.Join(dir,"SUMMARY_GR.txt"),[]byte(Summary(*r,p)))
}

// RunChecks never calls the build tool or creates an EXE. This safety invariant
// is covered by dedicated integration tests, not just by a button label.
func (s *Store) RunChecks(ctx context.Context,id string,progress func(string)) (Run,error) {
    r,p,m,base,hash,err:=s.stagedRun(id,StageChecks)
    if err!=nil{return r,err}
    dir:=filepath.Join(base,"runs",r.ID)
    work:=filepath.Join(dir,"work")
    if err=copyTree(filepath.Join(base,"source"),work);err!=nil{return r,err}
    execWork:=work
    if m.WorkDir!="" && m.WorkDir!="." {
        execWork=filepath.Join(work,filepath.FromSlash(m.WorkDir))
        info,e:=os.Stat(execWork)
        if e!=nil || !info.IsDir(){return r,errors.New("Δεν βρέθηκε ο φάκελος εργασίας")}
    }
    logs:=filepath.Join(dir,"logs")
    if err=os.MkdirAll(logs,0700);err!=nil{return r,err}
    add:=func(v Result){r.Results=append(r.Results,v);if progress!=nil{progress(fmt.Sprintf("%s: %s — %s",v.Phase,v.Name,v.Status))}}
    if len(m.Tests)==0 {add(Result{Name:"Δεν υπάρχουν λειτουργικά τεστ",Phase:"TEST",Status:"NOT RUN",Required:true,Reason:"Απαιτούνται τεκμηριωμένα λειτουργικά τεστ"})}
    for i,step:=range m.Tests {
        if ctx.Err()!=nil {add(Result{Name:step.Name,Phase:"TEST",Status:"CANCELLED",Required:step.Mandatory(),Reason:"Ακυρώθηκε"});continue}
        add(runStep(ctx,execWork,logs,"TEST",fmt.Sprintf("test-%d",i),step))
    }
    if len(m.Stress)==0 {add(Result{Name:"Δεν υπάρχουν stress tests",Phase:"STRESS",Status:"NOT RUN",Required:true,Reason:"Απαιτούνται τεστ πίεσης"})}
    for i,step:=range m.Stress {
        if ctx.Err()!=nil {add(Result{Name:step.Name,Phase:"STRESS",Status:"CANCELLED",Required:step.Mandatory(),Reason:"Ακυρώθηκε"});continue}
        deep:=DeepStressPlan(step)
        if progress!=nil {progress("ΠΛΑΝΟ ΠΙΕΣΗΣ: "+StressPlanDescription(deep))}
        add(runStress(ctx,execWork,logs,i,deep))
    }
    current,e:=HashTree(filepath.Join(base,"source"))
    if e!=nil{return r,e}
    if current!=hash {add(Result{Name:"Ακεραιότητα πηγών",Phase:"INTEGRITY",Status:"FAIL",Required:true,Reason:"Ο αρχικός κώδικας άλλαξε κατά τη διάρκεια των ελέγχων"})} else {
        add(Result{Name:"Ακεραιότητα πηγών",Phase:"INTEGRITY",Status:"PASS",Required:true})
    }
    r.Blocked=IsChecksBlocked(r)
    if err=s.saveStage(&r,p,base);err!=nil{return r,err}
    return r,nil
}

// LatestChecks ignores old combined-pipeline and build reports; an older PASS
// must not mask a newer failed preflight.
func (s *Store) LatestChecks(id string) (Run,error) {
    r,err:=s.latestStage(id,StageChecks)
    if err!=nil{return r,errors.New("Πρώτα εκτελέστε λειτουργικούς ελέγχους και stress tests")}
    return r,nil
}

func (s *Store) latestStage(id,stage string) (Run,error) {
    var latest Run
    base,err:=s.Base(id)
    if err!=nil{return latest,err}
    entries,err:=os.ReadDir(filepath.Join(base,"runs"))
    if err!=nil{return latest,err}
    found:=false
    for _,e:=range entries {
        if !e.IsDir(){continue}
        data,er:=os.ReadFile(filepath.Join(base,"runs",e.Name(),"results.json"))
        if er!=nil{continue}
        var candidate Run
        if json.Unmarshal(data,&candidate)!=nil||candidate.ProjectID!=id||candidate.Stage!=stage{continue}
        if !found||candidate.Started.After(latest.Started){latest=candidate;found=true}
    }
    if !found {return latest,errors.New("Δεν υπάρχει αποτέλεσμα για το στάδιο "+stage)}
    return latest,nil
}

func (s *Store) ReadyForBuild(id string) error {
    checks,err:=s.LatestChecks(id)
    if err!=nil{return err}
    if IsChecksBlocked(checks){return errors.New("Οι έλεγχοι ή τα stress tests δεν ολοκληρώθηκαν επιτυχώς")}
    _,m,err:=s.Get(id)
    if err!=nil{return err}
    if m.Build==nil{return errors.New("Το έργο δεν περιλαμβάνει οδηγίες κατασκευής EXE")}
    base,_:=s.Base(id)
    current,err:=HashTree(filepath.Join(base,"source"))
    if err!=nil{return err}
    if current!=checks.SourceHash || planDigest(m)!=checks.PlanHash {
        return errors.New("Ο κώδικας ή το σχέδιο δοκιμών άλλαξε — χρειάζεται νέος έλεγχος")
    }
    return nil
}

// BuildChecked is the only GUI-facing EXE build entrypoint. It requires a
// persisted successful check report and verifies the actual PE and startup.
func (s *Store) BuildChecked(ctx context.Context,id string,progress func(string))(Run,error){
    if err:=s.ReadyForBuild(id);err!=nil{return Run{},err}
    checks,_:=s.LatestChecks(id)
    r,p,m,base,hash,err:=s.stagedRun(id,StageBuild)
    if err!=nil{return r,err}
    dir:=filepath.Join(base,"runs",r.ID)
    work:=filepath.Join(dir,"work")
    if err=copyTree(filepath.Join(base,"source"),work);err!=nil{return r,err}
    execWork:=work
    if m.WorkDir!=""&&m.WorkDir!="." {execWork=filepath.Join(work,filepath.FromSlash(m.WorkDir))}
    logs:=filepath.Join(dir,"logs")
    if err=os.MkdirAll(logs,0700);err!=nil{return r,err}
    // Include the audited preflight results in the final release gate.
    r.Results=append(r.Results,checks.Results...)
    add:=func(v Result){r.Results=append(r.Results,v);if progress!=nil{progress(fmt.Sprintf("%s: %s — %s",v.Phase,v.Name,v.Status))}}
    if ctx.Err()!=nil{return r,ctx.Err()}
    oldExe,err:=exeInventory(work)
    if err!=nil{return r,err}
    var built Result
    if runtime.GOOS!="windows" {built=Result{Name:"Κατασκευή EXE",Phase:"BUILD",Status:"NOT RUN",Required:true,Reason:"Απαιτούνται Windows"}} else if msg:=missingBuildRequirement(ctx,execWork,*m.Build);msg!="" {
        built=Result{Name:m.Build.Name,Phase:"BUILD",Status:"NOT RUN",Required:true,Reason:msg}
    } else {
        built=runStep(ctx,execWork,logs,"BUILD","build",*m.Build)
        built.Required=true
    }
    if built.Status=="PASS" {
        after,e:=exeInventory(work)
        if e!=nil{built.Status="FAIL";built.Reason=e.Error()}
        for name,sum:=range after {
            if oldExe[name]==sum{continue}
            full:=filepath.Join(work,filepath.FromSlash(name))
            pefile,e:=pe.Open(full)
            if e!=nil{built.Status="FAIL";built.Reason="Μη έγκυρο Windows EXE: "+name;continue}
            pefile.Close()
            stat,e:=os.Stat(full)
            if e!=nil{built.Status="FAIL";built.Reason=e.Error();continue}
            if e=copyFile(full,filepath.Join(dir,"artifacts",filepath.FromSlash(name)));e!=nil{built.Status="FAIL";built.Reason=e.Error();continue}
            r.Artifacts=append(r.Artifacts,Artifact{Name:name,SHA256:sum,Bytes:stat.Size()})
        }
        if len(r.Artifacts)==0{built.Status="FAIL";built.Reason="Δεν δημιουργήθηκε νέο έγκυρο EXE"}
    }
    add(built)
    if built.Status=="PASS" {
        if m.Smoke==nil {add(Result{Name:"Έλεγχος πραγματικής εκκίνησης",Phase:"SMOKE",Status:"NOT RUN",Required:true,Reason:"Λείπει σχέδιο εκκίνησης"})} else {
            var smoke Result
            if m.Smoke.ExpectContains=="WINDOW_STAYS_OPEN" {smoke=runGUISmoke(ctx,execWork,*m.Smoke)} else {smoke=runStep(ctx,execWork,logs,"SMOKE","smoke",*m.Smoke)}
            smoke.Required=true
            add(smoke)
            for i,step:=range m.Visual {
                if smoke.Status!="PASS"{add(Result{Name:step.Name,Phase:"VISUAL",Status:"NOT RUN",Required:step.Mandatory(),Reason:"Απαιτείται επιτυχής εκκίνηση"});continue}
                result:=runStep(ctx,execWork,logs,"VISUAL",fmt.Sprintf("visual-%d",i),step)
                if result.Status=="PASS" && strings.Contains(strings.Join(step.Args," "),"visual_flicker_windows.py"){result=checkVisualResults(execWork,result)}
                add(result)
            }
        }
    }else {add(Result{Name:"Εκκίνηση EXE",Phase:"SMOKE",Status:"NOT RUN",Required:true,Reason:"Απέτυχε η κατασκευή"})}
    afterHash,e:=HashTree(filepath.Join(base,"source"))
    if e!=nil{return r,e}
    if afterHash!=hash || hash!=checks.SourceHash {add(Result{Name:"Ακεραιότητα πηγών μετά την κατασκευή",Phase:"INTEGRITY",Status:"FAIL",Required:true,Reason:"Μεταβλήθηκαν οι πηγές"})} 
    r.Blocked=IsBlocked(r)
    if err=s.saveStage(&r,p,base);err!=nil{return r,err}
    return r,nil
}

func (s *Store) ReadyForSetup(id string) error {
    if err:=s.ReadyForBuild(id);err!=nil{return err}
    build,err:=s.latestStage(id,StageBuild)
    if err!=nil{return errors.New("Πρώτα πρέπει να δημιουργηθεί και να ελεγχθεί επιτυχώς το EXE")}
    if IsBlocked(build){return errors.New("Το EXE δεν πέρασε όλους τους υποχρεωτικούς ελέγχους")}
    checks,_:=s.LatestChecks(id)
    if build.Started.Before(checks.Started) || build.SourceHash!=checks.SourceHash || build.PlanHash!=checks.PlanHash {return errors.New("Χρειάζεται νέα επιτυχής κατασκευή μετά τους τελευταίους ελέγχους")}
    _,m,err:=s.Get(id)
    if err!=nil{return err}
    if m.Setup==nil{return errors.New("Το έργο δεν περιλαμβάνει οδηγίες κατασκευής Setup. Δεν δημιουργούμε αυθαίρετο εγκαταστάτη.")}
    return nil
}

// A setup is optional and can only be generated after a verified EXE exists.
// It uses the exact tested build workspace, not the user's original sources.
func (s *Store) BuildSetup(ctx context.Context,id string,progress func(string))(Run,error){
    if err:=s.ReadyForSetup(id);err!=nil{return Run{},err}
    previous,_:=s.latestStage(id,StageBuild)
    r,p,m,base,hash,err:=s.stagedRun(id,StageSetup)
    if err!=nil{return r,err}
    dir:=filepath.Join(base,"runs",r.ID)
    work:=filepath.Join(dir,"work")
    if err=copyTree(filepath.Join(base,"runs",previous.ID,"work"),work);err!=nil{return r,err}
    execWork:=work
    if m.WorkDir!=""&&m.WorkDir!="."{execWork=filepath.Join(work,filepath.FromSlash(m.WorkDir))}
    before,err:=exeInventory(work)
    if err!=nil{return r,err}
    logs:=filepath.Join(dir,"logs")
    if err=os.MkdirAll(logs,0700);err!=nil{return r,err}
    result:=runStep(ctx,execWork,logs,"SETUP","setup",*m.Setup)
    result.Required=true
    if result.Status=="PASS" {
        after,e:=exeInventory(work)
        if e!=nil{result.Status="FAIL";result.Reason=e.Error()}
        for name,sum:=range after {
            if before[name]==sum{continue}
            full:=filepath.Join(work,filepath.FromSlash(name))
            f,e:=pe.Open(full)
            if e!=nil{result.Status="FAIL";result.Reason="Μη έγκυρο Setup EXE";continue}
            f.Close()
            st,e:=os.Stat(full)
            if e!=nil{result.Status="FAIL";result.Reason=e.Error();continue}
            if e=copyFile(full,filepath.Join(dir,"artifacts",filepath.FromSlash(name)));e!=nil{result.Status="FAIL";result.Reason=e.Error();continue}
            r.Artifacts=append(r.Artifacts,Artifact{Name:name,SHA256:sum,Bytes:st.Size()})
        }
        if len(r.Artifacts)==0{result.Status="FAIL";result.Reason="Η εντολή δεν δημιούργησε νέο Setup EXE"}
    }
    r.Results=append(r.Results,result)
    if progress!=nil{progress(fmt.Sprintf("SETUP: %s — %s",result.Name,result.Status))}
    current,e:=HashTree(filepath.Join(base,"source"))
    if e!=nil{return r,e}
    if current!=hash {r.Results=append(r.Results,Result{Name:"Ακεραιότητα",Phase:"INTEGRITY",Status:"FAIL",Required:true});result.Status="FAIL"}
    r.Blocked=result.Status!="PASS"
    if err=s.saveStage(&r,p,base);err!=nil{return r,err}
    return r,nil
}
