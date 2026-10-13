package engine

import (
 "encoding/json"
 "errors"
 "os"
 "path/filepath"
 "sort"
 "strings"
)

// Run history is analogous to a local CI run list. It is read-only and
// never promotes or changes a previously approved executable.
type RunIssue struct {
 Name string `json:"name"`
 Phase string `json:"phase"`
 Status string `json:"status"`
 Category string `json:"category"`
 Explanation string `json:"explanation"`
 Detail string `json:"detail,omitempty"`
 LogFile string `json:"log_file,omitempty"`
}

type HistoryEntry struct {
 Run Run `json:"run"`
 Summary string `json:"summary"`
 Issues []RunIssue `json:"issues"`
}

func resultCategory(r Result) (string,string) {
 if r.Status=="PASS" { return "PASS","Επιτυχής έλεγχος" }
 if r.Status=="CANCELLED" || r.FailureKind=="CANCELLED" {
  return "CANCELLED","Η δοκιμή ακυρώθηκε — δεν αποδεικνύει ελάττωμα του προγράμματος"
 }
 if r.FailureKind=="TIMEOUT" || strings.Contains(r.Reason,"Υπέρβαση χρονικού ορίου") || strings.Contains(r.Reason,"χρονικού ορίου ή ακύρωση") {
  return "TIMEOUT","Η δοκιμή δεν ολοκληρώθηκε εντός χρόνου — χρειάζεται έλεγχος διάρκειας και φορτίου"
 }
 if r.Status=="NOT RUN" {
  return "NOT_RUN","Η δοκιμή δεν εκτελέστηκε — εξετάστε τα απαιτούμενα εργαλεία ή τις προϋποθέσεις"
 }
 if r.Phase=="INTEGRITY" {
  return "INTEGRITY","Αλλοιώθηκαν πηγαία αρχεία ή στοιχεία επαλήθευσης"
 }
 return "TEST_FAILURE","Απέτυχε η δοκιμή — εξετάστε τα πραγματικά logs πριν αποδώσετε αιτία"
}

func DescribeRun(r Run) HistoryEntry {
 entry:=HistoryEntry{Run:r,Summary:"ΕΠΙΤΥΧΙΑ",Issues:[]RunIssue{}}
 if r.Blocked {entry.Summary="ΑΠΟΤΥΧΙΑ / ΔΕΝ ΕΓΚΡΙΘΗΚΕ"}
 for _,result:=range r.Results {
  if result.Status=="PASS" {continue}
  category,explanation:=resultCategory(result)
  entry.Issues=append(entry.Issues,RunIssue{
   Name:result.Name,Phase:result.Phase,Status:result.Status,Category:category,
   Explanation:explanation,Detail:result.Reason,LogFile:result.LogFile,
  })
 }
 return entry
}

func (s *Store) History(id string,limit int)([]HistoryEntry,error){
 base,err:=s.Base(id)
 if err!=nil{return nil,err}
 if _,_,err=s.Get(id);err!=nil{return nil,err}
 dir:=filepath.Join(base,"runs")
 entries,err:=os.ReadDir(dir)
 if errors.Is(err,os.ErrNotExist){return []HistoryEntry{},nil}
 if err!=nil{return nil,err}
 runs:=[]Run{}
 for _,f:=range entries {
  if !f.IsDir() || !validID(f.Name()) {continue}
  raw,err:=os.ReadFile(filepath.Join(dir,f.Name(),"results.json"))
  if err!=nil || len(raw)>4000000{continue}
  var run Run
  if json.Unmarshal(raw,&run)==nil && run.ID==f.Name() &&
    run.ProjectID==id && !run.Started.IsZero() {runs=append(runs,run)}
 }
 sort.Slice(runs,func(i,j int)bool{
  if runs[i].Started.Equal(runs[j].Started){return runs[i].ID>runs[j].ID}
  return runs[i].Started.After(runs[j].Started)
 })
 if limit<1 {limit=25}
 if limit>50 {limit=50}
 if len(runs)>limit{runs=runs[:limit]}
 out:=make([]HistoryEntry,0,len(runs))
 for _,run:=range runs{out=append(out,DescribeRun(run))}
 return out,nil
}
