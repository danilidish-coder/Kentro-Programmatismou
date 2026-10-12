package engine

import (
    "os"
    "path/filepath"
    "strings"
    "sync"
    "testing"
)

// New imports are never deleted by removing them from the user's active list.
func TestArchiveAndRestoreProjectPreservesAllData(t *testing.T) {
    s := newTestStore(t)
    first, err := s.ImportZIP(zipFixture(t, map[string]string{
        "main.go": "package main\nfunc main() {}\n",
        "extra/keep.txt": "important data",
    }))
    if err != nil { t.Fatal(err) }
    root, err := s.Base(first.ID)
    if err != nil { t.Fatal(err) }
    if err := os.MkdirAll(filepath.Join(root,"runs","run1"),0700); err != nil { t.Fatal(err) }
    if err := os.WriteFile(filepath.Join(root,"runs","run1","results.json"),[]byte("history-safe"),0600); err != nil {t.Fatal(err)}
    if err := os.MkdirAll(filepath.Join(root,"stable","s1"),0700); err != nil { t.Fatal(err) }
    if err := os.WriteFile(filepath.Join(root,"stable","s1","approved.txt"),[]byte("approved-safe"),0600); err != nil {t.Fatal(err)}

    archive, err := s.ArchiveProject(first.ID)
    if err != nil { t.Fatal(err) }
    if !strings.Contains(archive, string(filepath.Separator)+"removed"+string(filepath.Separator)) { t.Fatal("not archived in removed directory", archive) }
    listed, err := s.List()
    if err != nil || len(listed) != 0 {t.Fatalf("project still active: %+v, %v",listed,err)}
    if _, err := os.Stat(root); !os.IsNotExist(err) { t.Fatalf("active source still exists: %v",err) }
    for path, expected := range map[string]string{
        "source/main.go":"package main\nfunc main() {}\n",
        "source/extra/keep.txt":"important data",
        "runs/run1/results.json":"history-safe",
        "stable/s1/approved.txt":"approved-safe",
    } {
        data, err := os.ReadFile(filepath.Join(archive,filepath.FromSlash(path)))
        if err != nil || string(data) != expected { t.Fatalf("lost %s: %q, %v", path, data,err) }
    }
    restored, err := s.RestoreLastRemoved()
    if err != nil {t.Fatal(err)}
    if restored.ID != first.ID {t.Fatal("restored wrong id", restored.ID)}
    listed, err = s.List()
    if err != nil || len(listed) != 1 || listed[0].ID != first.ID {t.Fatalf("restored project absent: %+v %v",listed,err)}
    for _, path := range []string{"source/main.go","source/extra/keep.txt","runs/run1/results.json","stable/s1/approved.txt"} {
        if _, err := os.Stat(filepath.Join(root,filepath.FromSlash(path))); err != nil {t.Fatal(path,err)}
    }
}

func TestArchiveRejectsUnknownOrInvalidID(t *testing.T) {
    s := newTestStore(t)
    for _, id := range []string{"", "../unsafe", "112233445566778899aabbcc", "A0123456"} {
        if _, err := s.ArchiveProject(id); err == nil {t.Fatalf("accepted archive of %q",id)}
    }
    if _, err := s.RestoreLastRemoved(); err == nil {t.Fatal("unexpected restore without removal")}
}
func TestRestoresNewestRemovedProject(t *testing.T) {
    s := newTestStore(t)
    p1,err:=s.ImportZIP(zipFixture(t,map[string]string{"a.txt":"one"}))
    if err!=nil {t.Fatal(err)}
    p2,err:=s.ImportZIP(zipFixture(t,map[string]string{"a.txt":"two"}))
    if err!=nil {t.Fatal(err)}
    if _,err=s.ArchiveProject(p1.ID);err!=nil{t.Fatal(err)}
    if _,err=s.ArchiveProject(p2.ID);err!=nil{t.Fatal(err)}
    p,err:=s.RestoreLastRemoved()
    if err!=nil{t.Fatal(err)}
    if p.ID!=p2.ID{t.Fatalf("restored %q rather than newest %q",p.ID,p2.ID)}
    p,err=s.RestoreLastRemoved()
    if err!=nil{t.Fatal(err)}
    if p.ID!=p1.ID{t.Fatal("old archive failed to restore")}
}

func TestConcurrentListDoesNotSeeHalfArchivedData(t *testing.T) {
    s:=newTestStore(t)
    p,err:=s.ImportZIP(zipFixture(t,map[string]string{"main.py":"print(1)"}))
    if err!=nil{t.Fatal(err)}
    var wg sync.WaitGroup
    errch:=make(chan error,50)
    for i:=0;i<5;i++ {
        wg.Add(1)
        go func(){
            defer wg.Done()
            for j:=0;j<10;j++ {
                _,err:=s.List()
                if err!=nil{errch<-err}
            }
        }()
    }
    if _,err:=s.ArchiveProject(p.ID);err!=nil{t.Fatal(err)}
    wg.Wait()
    close(errch)
    for err:=range errch{t.Fatal(err)}
}
