package engine

// Explicit reviewer checkpoint between the local CHECKS stage and the EXE
// compiler. The reviewer receives the report outside this application and
// supplies a matching JSON decision. This is a user-mediated process gate,
// not a cryptographic attestation of reviewer identity.
import (
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "strings"
)

const reviewSchema = "kentro-review-v1"
const reviewDecision = "APPROVED_FOR_BUILD"

type ReviewTicket struct {
 Schema        string `json:"schema"`
 Decision      string `json:"decision"`
 ProjectID     string `json:"project_id"`
 RunID         string `json:"run_id"`
 SourceSHA256  string `json:"source_sha256"`
 PlanSHA256    string `json:"plan_sha256"`
 ResultsSHA256 string `json:"results_sha256"`
 Reviewer      string `json:"reviewer"`
 Note          string `json:"note,omitempty"`
}

type ReviewStatus struct {
 Status  string `json:"status"`
 Message string `json:"message"`
 RunID   string `json:"run_id,omitempty"`
}

func (s *Store) reviewRequest(id string) (ReviewTicket,error) {
 var empty ReviewTicket
 checks,err:=s.LatestChecks(id)
 if err!=nil{return empty,err}
 if IsChecksBlocked(checks) {return empty,errors.New("Οι υποχρεωτικοί έλεγχοι απέτυχαν. Δεν επιτρέπεται εξωτερική έγκριση.")}
 _,m,err:=s.Get(id)
 if err!=nil{return empty,err}
 base,err:=s.Base(id)
 if err!=nil{return empty,err}
 // Final release approval requires a plan explicitly prepared for this project.
 // Automatic discovery is useful for preview, never sufficient for sign-off.
 planFile:=filepath.Join(base,"source","project-manifest.json")
 planInfo,err:=os.Stat(planFile)
 if err!=nil || !planInfo.Mode().IsRegular() {
  return empty,errors.New("Απαιτείται ρητό σχέδιο ελέγχων project-manifest.json από τον βοηθό")
 }
 current,err:=HashTree(filepath.Join(base,"source"))
 if err!=nil{return empty,err}
 if current!=checks.SourceHash || planDigest(m)!=checks.PlanHash {
  return empty,errors.New("Ο κώδικας ή το σχέδιο ελέγχων άλλαξε. Απαιτείται νέα εκτέλεση τεστ.")
 }
 raw,err:=os.ReadFile(filepath.Join(base,"runs",checks.ID,"results.json"))
 if err!=nil{return empty,err}
 if len(raw)==0 || len(raw)>4000000 {return empty,errors.New("Μη έγκυρη αποθηκευμένη αναφορά ελέγχων")}
 var saved Run
 if err=json.Unmarshal(raw,&saved);err!=nil {return empty,err}
 if saved.ID!=checks.ID || saved.ProjectID!=id ||
  saved.Stage!=StageChecks || IsChecksBlocked(saved) ||
  saved.SourceHash!=checks.SourceHash || saved.PlanHash!=checks.PlanHash {
  return empty,errors.New("Ασυμφωνία αποθηκευμένων αποτελεσμάτων ελέγχου")
 }
 sum:=sha256.Sum256(raw)
 return ReviewTicket{
  Schema:reviewSchema,Decision:reviewDecision,ProjectID:id,
  RunID:checks.ID,SourceSHA256:checks.SourceHash,PlanSHA256:checks.PlanHash,
  ResultsSHA256:hex.EncodeToString(sum[:]),Reviewer:"ChatGPT",
 },nil
}

// ReviewRequest contains fingerprints only, not a prefilled approval decision.
// The reviewer supplies a separate explicit decision after inspecting the report.
type ReviewRequestInfo struct {
 Schema string `json:"schema"`
 ProjectID string `json:"project_id"`
 RunID string `json:"run_id"`
 SourceSHA256 string `json:"source_sha256"`
 PlanSHA256 string `json:"plan_sha256"`
 ResultsSHA256 string `json:"results_sha256"`
}
func (s *Store) ReviewRequest(id string)(ReviewRequestInfo,error){
 req,err:=s.reviewRequest(id)
 if err!=nil{return ReviewRequestInfo{},err}
 return ReviewRequestInfo{
  Schema:req.Schema,ProjectID:req.ProjectID,RunID:req.RunID,
  SourceSHA256:req.SourceSHA256,PlanSHA256:req.PlanSHA256,
  ResultsSHA256:req.ResultsSHA256,
 },nil
}

func (s *Store) reviewPath(id,runID string)(string,error){
 if !validID(id) || !validID(runID){return "",errors.New("Μη έγκυρο αναγνωριστικό έγκρισης")}
 base,err:=s.Base(id)
 if err!=nil{return "",err}
 return filepath.Join(base,"reviews",runID+".json"),nil
}

func validateReviewTicket(got,expected ReviewTicket)error{
 if got.Schema!=reviewSchema || got.Decision!=reviewDecision {
  return errors.New("Απορρίφθηκε: η απόφαση δεν είναι έγκριση κατασκευής")
 }
 if got.ProjectID!=expected.ProjectID || got.RunID!=expected.RunID ||
  got.SourceSHA256!=expected.SourceSHA256 ||
  got.PlanSHA256!=expected.PlanSHA256 ||
  got.ResultsSHA256!=expected.ResultsSHA256 {
  return errors.New("Η έγκριση αφορά διαφορετικό έργο, τεστ ή έκδοση κώδικα")
 }
 if got.Reviewer!="ChatGPT" || len(strings.TrimSpace(got.Note))<8 ||
  len(got.Note)>2000 {
  return errors.New("Απαιτείται σημείωμα επανεξέτασης από το ChatGPT")
 }
 return nil
}

// ImportReview accepts a reviewer-provided JSON file, checks binding to the
// latest successful CHECKS run, then saves the decision separately from source.
// No build is performed and no source hash is modified.
func (s *Store) ImportReview(id,file string)(ReviewStatus,error){
 status:=ReviewStatus{Status:"WAITING_REVIEW"}
 req,err:=s.reviewRequest(id)
 if err!=nil{return status,err}
 f,err:=os.Open(file)
 if err!=nil{return status,err}
 defer f.Close()
 st,err:=f.Stat()
 if err!=nil{return status,err}
 if !st.Mode().IsRegular() || st.Size()>16384 || st.Size()==0 {
  return status,errors.New("Το αρχείο έγκρισης δεν είναι έγκυρο")
 }
 dec:=json.NewDecoder(f)
 dec.DisallowUnknownFields()
 var decision ReviewTicket
 if err=dec.Decode(&decision);err!=nil{return status,fmt.Errorf("Μη έγκυρο JSON έγκρισης: %w",err)}
 var extra any
 if err=dec.Decode(&extra);err!=nil {
  if err!=io.EOF{return status,errors.New("Μη επιτρεπτά πρόσθετα δεδομένα JSON")}
 } else {return status,errors.New("Το αρχείο έγκρισης περιέχει πρόσθετα στοιχεία JSON")}
 if err=validateReviewTicket(decision,req);err!=nil{return status,err}
 path,err:=s.reviewPath(id,req.RunID)
 if err!=nil{return status,err}
 if err=atomicJSON(path,decision);err!=nil{return status,err}
 // Verify on disk before reporting readiness.
 if err=s.checkReview(id);err!=nil{return ReviewStatus{Status:"WAITING_REVIEW"},err}
 return ReviewStatus{Status:"APPROVED",Message:"Ελήφθη η έγκριση για την τελευταία δοκιμασμένη έκδοση",RunID:req.RunID},nil
}

// The check is inside the engine gate, not only a disabled WPF button.
func (s *Store) checkReview(id string)error{
 req,err:=s.reviewRequest(id)
 if err!=nil{return err}
 path,err:=s.reviewPath(id,req.RunID)
 if err!=nil{return err}
 b,err:=os.ReadFile(path)
 if errors.Is(err,os.ErrNotExist){
  return errors.New("Αναμονή έγκρισης ChatGPT: εξάγετε την αναφορά, στείλτε την για εξέταση και εισαγάγετε την απόφαση")
 }
 if err!=nil{return err}
 if len(b)==0 || len(b)>16384{return errors.New("Μη έγκυρο μέγεθος έγκρισης")}
 var ticket ReviewTicket
 if err=json.Unmarshal(b,&ticket);err!=nil{return err}
 return validateReviewTicket(ticket,req)
}

func (s *Store) GetReviewStatus(id string)ReviewStatus{
 checks,e:=s.LatestChecks(id)
 if e!=nil{return ReviewStatus{Status:"WAITING_TESTS",Message:"Πρώτα απαιτούνται έλεγχοι"}}
 if IsChecksBlocked(checks) {
  return ReviewStatus{Status:"FAILED_TESTS",Message:"Απέτυχαν υποχρεωτικοί έλεγχοι — δεν επιτρέπεται έγκριση",RunID:checks.ID}
 }
 _,e=s.reviewRequest(id)
 if e!=nil{return ReviewStatus{Status:"STALE",Message:e.Error(),RunID:checks.ID}}
 if e=s.checkReview(id);e!=nil{
  return ReviewStatus{Status:"WAITING_REVIEW",Message:"Οι έλεγχοι πέρασαν. Απαιτείται ρητή έγκριση ChatGPT για EXE.",RunID:checks.ID}
 }
 return ReviewStatus{Status:"APPROVED",Message:"Έγκριση ελέγχων καταχωρίστηκε — επιτρέπεται κατασκευή EXE",RunID:checks.ID}
}
