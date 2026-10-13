package main

// This is the strictly local JSON-lines bridge for the native WinUI 3 front end.
// The pre-existing engine remains the only authority for test/build/release gates.
import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "kentro.local/programmatismou/engine"
 "os"
 "sync"
)

type message struct {
 Type string `json:"type"`
 Payload any `json:"payload,omitempty"`
 Message string `json:"message,omitempty"`
}
var outputMu sync.Mutex
func emit(kind string, payload any, detail string) {
 outputMu.Lock()
 defer outputMu.Unlock()
 _ = json.NewEncoder(os.Stdout).Encode(message{Type:kind,Payload:payload,Message:detail})
}
func arg(n int) string {
 if len(os.Args)>n {return os.Args[n]}
 return ""
}
func require(v string) (string,error) {
 if v=="" {return "",errors.New("Λείπει υποχρεωτικό αναγνωριστικό ή αρχείο")}
 return v,nil
}
func execute() (any,error) {
 action:=arg(1)
 if action=="version" { return map[string]string{"version":engine.ProductVersion},nil }
 store,err:=engine.NewStore("")
 if err!=nil{return nil,err}
 id:=arg(2)
 progress:=func(s string){emit("progress",nil,s)}
 switch action {
 case "list":
   ps,e:=store.List(); if e!=nil{return nil,e}; if ps==nil{ps=[]engine.Project{}}
   return ps,nil
 case "preview":
   p,e:=require(id);if e!=nil{return nil,e}
   return engine.PreviewZIP(p)
 case "import":
   p,e:=require(id);if e!=nil{return nil,e}
   if _,e=engine.PreviewZIP(p);e!=nil{return nil,e}
   return store.ImportZIP(p)
 case "history":
   if _,e:=require(id);e!=nil{return nil,e}
   return store.History(id,25)
 case "details":
   if _,e:=require(id);e!=nil{return nil,e}
   p,m,e:=store.Get(id);if e!=nil{return nil,e}
   latest,_:=store.LatestRun(id)
   checks,_:=store.LatestChecks(id)
   readyBuild:=store.ReadyForBuild(id)==nil
   readySetup:=store.ReadyForSetup(id)==nil
   reviewStatus:=store.GetReviewStatus(id)
   root,_:=store.Base(id)
   return map[string]any{"project":p,"manifest":m,"latest":latest,"checks":checks,
      "ready_build":readyBuild,"ready_setup":readySetup,"review_status":reviewStatus,"folder":root},nil
 case "review-request":
   if _,e:=require(id);e!=nil{return nil,e}
   return store.ReviewRequest(id)
 case "review-import":
   if _,e:=require(id);e!=nil{return nil,e}
   path,e:=require(arg(3));if e!=nil{return nil,e}
   return store.ImportReview(id,path)
 case "checks":
   if _,e:=require(id);e!=nil{return nil,e}
   return store.RunChecks(context.Background(),id,progress)
 case "build":
   if _,e:=require(id);e!=nil{return nil,e}
   if e:=store.ReadyForBuild(id);e!=nil{return nil,e}
   r,e:=store.BuildChecked(context.Background(),id,progress)
   if e!=nil{return nil,e}
   if r.Blocked{return map[string]any{"run":r,"approved":false},nil}
   stable,e:=store.Approve(id)
   if e!=nil{return nil,e}
   return map[string]any{"run":r,"approved":true,"stable_folder":stable},nil
 case "setup":
   if _,e:=require(id);e!=nil{return nil,e}
   if e:=store.ReadyForSetup(id);e!=nil{return nil,e}
   return store.BuildSetup(context.Background(),id,progress)
 case "report":
   if _,e:=require(id);e!=nil{return nil,e}
   path,e:=store.ExportLatest(id);if e!=nil{return nil,e}
   return map[string]string{"path":path},nil
 case "approve":
   if _,e:=require(id);e!=nil{return nil,e}
   path,e:=store.Approve(id);if e!=nil{return nil,e}
   return map[string]string{"path":path},nil
 case "backup":
   if _,e:=require(id);e!=nil{return nil,e}
   path,e:=store.Backup(id);if e!=nil{return nil,e}
   return map[string]string{"path":path},nil
 case "trash":
   if _,e:=require(id);e!=nil{return nil,e}
   name,e:=store.TrashProject(id);if e!=nil{return nil,e}
   return map[string]string{"name":name},nil
 case "restore":
   return store.RestoreLatestTrashed()
 case "folder":
   if _,e:=require(id);e!=nil{return nil,e}
   folder,e:=store.Base(id);if e!=nil{return nil,e}
   if _,e=os.Stat(folder);e!=nil{return nil,e}
   return map[string]string{"path":folder},nil
 default: return nil,fmt.Errorf("Άγνωστη ενέργεια: %s",action)
 }
}
func main() {
 defer func(){if r:=recover();r!=nil{emit("error",nil,fmt.Sprint(r));os.Exit(1)}}()
 result,err:=execute()
 if err!=nil{emit("error",nil,err.Error());os.Exit(1)}
 emit("result",result,"")
}
