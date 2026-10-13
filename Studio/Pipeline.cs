using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.IO.Compression;
using System.Linq;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using System.Threading;
using System.Threading.Tasks;

namespace KentroOffline;

public static class Pipeline
{
 private static readonly SemaphoreSlim Exclusive=new(1,1);

 private static string PlanHash(string id)=>Workspace.HashFile(
  Path.Combine(Workspace.SourceOf(id),"project-manifest.json"));
 private static string CheckStatus(RunRecord r)=>
  r.Results.All(x=>!x.Required||x.Status=="PASS")&&r.Results.Any(x=>x.Phase=="INTEGRITY"&&x.Status=="PASS")
  ? "PASS" : "BLOCKED";
 private static void Emit(Action<string>? progress,string line)
 {
  progress?.Invoke($"[{DateTime.Now:HH:mm:ss}] {line}");
 }

 private static string PathFor(string workdir,string sourceRoot)
 {
  if(workdir==""||workdir==".")return sourceRoot;
  return Workspace.SafeInside(sourceRoot,workdir.Replace('\\','/'));
 }
 private static string MakeWorkspace(string id,string runId,string suffix)
 {
  var root=Path.Combine(Workspace.RunDir(id,runId),"work",suffix);
  Workspace.CopyTree(Workspace.SourceOf(id),root);
  return root;
 }

 private static async Task<TestResult> ExecuteAsync(Step step,string phase,string root,string logDir,string logName,
  CancellationToken token,Action<string>? progress=null)
 {
  var res=new TestResult{
   Module=step.Module,Name=step.Name,Phase=phase,Required=step.Required,
   Status="NOT_RUN",Category="ENVIRONMENT",LogFile=logName+".txt"};
  string file;List<string> args;
  try{
   (file,args)=Toolchains.Resolve(step.Command,step.Args,root);
  }catch(Exception e){res.Reason=e.Message;return res;}
  string cwd=PathFor(".",root);
  var psi=new ProcessStartInfo(file){
   WorkingDirectory=cwd,UseShellExecute=false,RedirectStandardOutput=true,
   RedirectStandardError=true,CreateNoWindow=true
  };
  foreach(string arg in args)psi.ArgumentList.Add(arg);
  Toolchains.Configure(psi);
  var sb=new StringBuilder();
  var gate=new object();
  void AddLine(string? line)
  {
   if(line is null)return;
   lock(gate){if(sb.Length<1200000)sb.AppendLine(line);}
  }
  using var process=new Process{StartInfo=psi,EnableRaisingEvents=true};
  process.OutputDataReceived+=(_,e)=>AddLine(e.Data);
  process.ErrorDataReceived+=(_,e)=>AddLine(e.Data);
  var sw=Stopwatch.StartNew();
  bool timeout=false;
  bool cancelled=false;
  try{
   if(!process.Start())throw new IOException("Δεν εκκινήθηκε η εντολή");
   process.BeginOutputReadLine();
   process.BeginErrorReadLine();
   using var limit=CancellationTokenSource.CreateLinkedTokenSource(token);
   limit.CancelAfter(TimeSpan.FromSeconds(step.TimeoutSeconds));
   try{await process.WaitForExitAsync(limit.Token);}
   catch(OperationCanceledException){
    timeout=!token.IsCancellationRequested;
    cancelled=token.IsCancellationRequested;
    try{process.Kill(entireProcessTree:true);}catch{}
    using var cleanup=new CancellationTokenSource(TimeSpan.FromSeconds(15));
    try{await process.WaitForExitAsync(cleanup.Token);}catch{}
   }
   if(cancelled){
    res.Status="CANCELLED";res.Category="CANCELLED";
    res.Reason="Ο χειριστής ακύρωσε την εκτέλεση";
   }else if(timeout){
    res.Status="FAIL";res.Category="TIMEOUT";
    res.Reason="Υπέρβαση "+step.TimeoutSeconds+" δευτερολέπτων";
   }else if(process.ExitCode!=0){
    res.Status="FAIL";res.Category="COMMAND_ERROR";
    res.Reason="Κωδικός εξόδου "+process.ExitCode;
   }else if(step.ExpectContains.Length>0&&!sb.ToString().Contains(step.ExpectContains,StringComparison.Ordinal)){
    res.Status="FAIL";res.Category="ASSERTION";
    res.Reason="Το τεστ δεν έδωσε το αναμενόμενο αποτέλεσμα";
   }else{
    res.Status="PASS";res.Category="PASS";
    res.Reason="Ο έλεγχος ολοκληρώθηκε";
   }
  }catch(Exception ex){
   res.Status="NOT_RUN";res.Category="ENVIRONMENT";res.Reason=ex.Message;
  }finally{
   sw.Stop();res.ElapsedMs=sw.ElapsedMilliseconds;
   Directory.CreateDirectory(logDir);
   string path=Workspace.SafeInside(logDir,res.LogFile);
   var content=$"{step.Name}\n{phase} · {res.Status} · {res.Category}\n"+
    $"{file} {string.Join(" ",args)}\n{res.Reason}\n{sw.ElapsedMilliseconds} ms\n\n{sb}";
   if(content.Length>1400000)content=content[..1400000]+"\n[truncated]\n";
   await File.WriteAllTextAsync(path,content,new UTF8Encoding(false),CancellationToken.None);
   Emit(progress,$"{phase}: {step.Name} — {res.Status} ({res.ElapsedMs} ms)");
  }
  return res;
 }

 private static async Task<TestResult> RunStressAsync(Stress stress,string id,string runId,int number,
  string logs,CancellationToken token,Action<string>? progress)
 {
  var specs=new List<TestResult>();
  int parallel=stress.Kind=="parallel"?Math.Min(12,stress.Parallel):1;
  int repeats=stress.Repeats;
  if(stress.Kind=="sequential"||stress.Kind=="restart")repeats=Math.Max(4,repeats);
  else{repeats=Math.Max(6,repeats);parallel=Math.Max(3,parallel);}
  var semaphore=new SemaphoreSlim(parallel);
  var tasks=new List<Task<TestResult>>();
  // Each script runs on its own unmodified workspace. A failure or a timeout
  // can never corrupt either the original project or another script's files.
  for(int i=0;i<repeats;i++){
   int n=i;
   await semaphore.WaitAsync(token);
   tasks.Add(Task.Run(async()=>{
    try{
     string root=MakeWorkspace(id,runId,$"stress-{number}-{n}");
     var step=new Step{
      Name=stress.Name+" #"+(n+1),
      Module=stress.Module,Command=stress.Command,Args=stress.Args,
      TimeoutSeconds=stress.TimeoutSeconds,Required=stress.Required,
      ExpectContains=stress.ExpectContains
     };
     if(stress.Kind=="restart")return await ForcedRestartAsync(stress,step,root,logs,
      $"stress-{number}-{n}",token,progress);
     return await ExecuteAsync(step,"STRESS",root,logs,$"stress-{number}-{n}",token,progress);
    }finally{semaphore.Release();}
   },token));
  }
  TestResult[] results=await Task.WhenAll(tasks);
  int fail=results.Count(x=>x.Status!="PASS");
  var first=results.FirstOrDefault(x=>x.Status!="PASS");
  string kind=stress.Kind switch{"sequential"=>"διαδοχικές πλήρεις","restart"=>"αναγκαστικές επανεκκινήσεις",_=>"παράλληλες"};
  return new TestResult{
   Name=stress.Name,Module=stress.Module,Phase="STRESS",
   Required=stress.Required,Status=fail==0?"PASS":"FAIL",
   Category=first?.Category??"PASS",
   Reason=fail==0
    ?$"{repeats} {kind} εκτελέσεις · έως {parallel} παράλληλες"
    :$"{fail}/{repeats} αποτυχίες ({kind}) · {first?.Reason}",
   ElapsedMs=results.Sum(x=>x.ElapsedMs),
   LogFile=results.FirstOrDefault()?.LogFile??""
  };
 }

 private static async Task<TestResult> ForcedRestartAsync(Stress stress,Step step,string work,
  string logs,string logName,CancellationToken ct,Action<string>? progress)
 {
  // A restart is genuinely a forced termination followed by a separately
  // configured health check. It never masquerades as completed Python tests.
  if(stress.Health==null)
   return new TestResult{Phase="STRESS",Name=stress.Name,Module=stress.Module,Status="FAIL",
    Category="INVALID_PLAN",Reason="Το restart χρειάζεται health check"};
  string file;List<string> args;
  try{(file,args)=Toolchains.Resolve(step.Command,step.Args,work);}
  catch(Exception ex){return new TestResult{Phase="STRESS",Name=stress.Name,Status="NOT_RUN",Category="ENVIRONMENT",Reason=ex.Message};}
  var psi=new ProcessStartInfo(file){WorkingDirectory=work,UseShellExecute=false,CreateNoWindow=true};
  foreach(var a in args)psi.ArgumentList.Add(a);
  Toolchains.Configure(psi);
  using var process=Process.Start(psi)??throw new IOException("Δεν ξεκίνησε το stress process");
  try{await Task.Delay(800,ct);}
  finally{try{if(!process.HasExited)process.Kill(true);}catch{}}
  var check=await ExecuteAsync(stress.Health,"HEALTH",work,logs,logName+"-health",ct,progress);
  check.Phase="STRESS";
  check.Name=stress.Name;
  return check;
 }

 public static async Task<RunRecord> ChecksAsync(string id,Action<string>? progress,CancellationToken token=default)
 {
  await Exclusive.WaitAsync(token);
  try{
   var plan=Workspace.ReadPlan(id);
   await Toolchains.EnsureAsync(progress,token);
   var record=new RunRecord{Id=Workspace.NewId(),ProjectId=id,Stage="CHECKS",
     SourceSha256=Workspace.HashTree(Workspace.SourceOf(id)),PlanSha256=PlanHash(id)};
   string runDir=Workspace.RunDir(id,record.Id);
   Directory.CreateDirectory(runDir);
   string logs=Path.Combine(runDir,"logs");
   try{
    Emit(progress,$"Έλεγχοι {plan.Name} {plan.Version}: {plan.Tests.Count} tests, {plan.Stress.Count} stress");
    int count=0;
    foreach(var step in plan.Tests){
     token.ThrowIfCancellationRequested();
     string work=MakeWorkspace(id,record.Id,"test-"+count);
     record.Results.Add(await ExecuteAsync(step,"TEST",work,logs,$"test-{count}",token,progress));
     count++;
    }
    for(int i=0;i<plan.Stress.Count;i++){
     token.ThrowIfCancellationRequested();
     record.Results.Add(await RunStressAsync(plan.Stress[i],id,record.Id,i,logs,token,progress));
    }
    string actual=Workspace.HashTree(Workspace.SourceOf(id));
    string actualPlan=PlanHash(id);
    bool same=actual==record.SourceSha256&&actualPlan==record.PlanSha256;
    record.Results.Add(new TestResult{Module="SECURITY",Name="Ακεραιότητα πηγαίου κώδικα και πλάνου",
     Phase="INTEGRITY",Required=true,Status=same?"PASS":"FAIL",
     Category=same?"PASS":"INTEGRITY",Reason=same?"Κώδικας και σχέδιο αμετάβλητα":"Το έργο τροποποιήθηκε κατά τη διάρκεια των ελέγχων"});
    record.Blocked=CheckStatus(record)!="PASS";
    record.Status=record.Blocked?"ΔΕΝ ΕΓΚΡΙΝΕΤΑΙ":"ΟΛΟΚΛΗΡΩΘΗΚΑΝ · ΑΝΑΜΟΝΗ ΕΓΚΡΙΣΗΣ";
   }catch(OperationCanceledException){
    record.Blocked=true;record.Status="ΑΚΥΡΩΘΗΚΕ";
    record.Results.Add(new TestResult{Module="USER",Name="Ακύρωση χειριστή",Status="CANCELLED",Category="CANCELLED",Required=true,Phase="TEST"});
   }catch(Exception ex){
    record.Blocked=true;record.Status="ΣΦΑΛΜΑ ΕΚΤΕΛΕΣΗΣ";
    record.Results.Add(new TestResult{Module="ENGINE",Name="Σφάλμα εκτέλεσης",Status="FAIL",Category="ENGINE",
      Reason=ex.Message,Phase="TEST",Required=true});
   }finally{record.Finished=DateTime.UtcNow;Workspace.SaveRun(record);}
   return record;
  }finally{Exclusive.Release();}
 }

 public static ReviewRequest GetReviewRequest(string id)
 {
  var checks=Workspace.LastChecks(id)??throw new InvalidOperationException("Δεν υπάρχουν έλεγχοι");
  string invalidated=Path.Combine(Workspace.DirectoryOf(id),"checks-invalidated.flag");
  if(File.Exists(invalidated)&&
     long.TryParse(File.ReadAllText(invalidated),out long resetTicks)&&
     checks.Started.Ticks<=resetTicks)
   throw new InvalidOperationException("Έγινε επαναφορά έκδοσης μετά τους ελέγχους: απαιτούνται νέα τεστ και νέα έγκριση");
  if(checks.Blocked||checks.Status=="ΑΚΥΡΩΘΗΚΕ")
   throw new InvalidOperationException("Οι έλεγχοι απέτυχαν. Δεν μπορεί να δοθεί έγκριση");
  if(checks.SourceSha256!=Workspace.HashTree(Workspace.SourceOf(id))||
    checks.PlanSha256!=PlanHash(id))
   throw new InvalidOperationException("Ο πηγαίος κώδικας ή το σχέδιο άλλαξαν. Απαιτούνται νέοι έλεγχοι");
  string resultFile=Path.Combine(Workspace.RunDir(id,checks.Id),"results.json");
  return new ReviewRequest{
   ProjectId=id,RunId=checks.Id,SourceSha256=checks.SourceSha256,
   PlanSha256=checks.PlanSha256,ResultsSha256=Workspace.HashFile(resultFile)
  };
 }
 private static string ReviewPath(string id,string runId)=>
  Path.Combine(Workspace.DirectoryOf(id),"approvals",runId+".json");

 public static void ImportApproval(string id,string path)
 {
  var expected=GetReviewRequest(id);
  if(new FileInfo(path).Length>16384)throw new InvalidDataException("Υπερβολικά μεγάλο αρχείο έγκρισης");
  var decision=Workspace.LoadJson<ReviewDecision>(path);
  if(decision.Schema!="kentro-review-v1"||decision.Decision!="APPROVED_FOR_BUILD"||
     decision.Reviewer!="ChatGPT"||decision.Note.Trim().Length<8 ||
     decision.ProjectId!=expected.ProjectId||decision.RunId!=expected.RunId||
     decision.SourceSha256!=expected.SourceSha256||decision.PlanSha256!=expected.PlanSha256||
     decision.ResultsSha256!=expected.ResultsSha256)
   throw new InvalidDataException("Η έγκριση δεν αντιστοιχεί στην ακριβή έκδοση, στα τεστ και στην αναφορά");
  Workspace.SaveJson(ReviewPath(id,expected.RunId),decision);
 }

 public static bool HasApproval(string id)
 {
  try{
   var expected=GetReviewRequest(id);
   string file=ReviewPath(id,expected.RunId);
   if(!File.Exists(file))return false;
   var dec=Workspace.LoadJson<ReviewDecision>(file);
   return dec.Schema=="kentro-review-v1"&&dec.Decision=="APPROVED_FOR_BUILD"&&
    dec.Reviewer=="ChatGPT"&&dec.Note.Trim().Length>=8&&dec.ProjectId==expected.ProjectId&&
    dec.RunId==expected.RunId&&dec.SourceSha256==expected.SourceSha256&&
    dec.PlanSha256==expected.PlanSha256&&dec.ResultsSha256==expected.ResultsSha256;
  }catch{return false;}
 }

 public static string ExportReport(string id,string target)
 {
  var checks=Workspace.LastChecks(id)??throw new InvalidOperationException("Δεν έχουν γίνει ακόμη έλεγχοι");
  string source=Workspace.RunDir(id,checks.Id);
  string results=Path.Combine(source,"results.json");
  Directory.CreateDirectory(Path.GetDirectoryName(target)!);
  if(File.Exists(target))File.Delete(target);
  using var z=ZipFile.Open(target,ZipArchiveMode.Create);
  z.CreateEntryFromFile(results,"results.json");
  string manifest=Path.Combine(Workspace.SourceOf(id),"project-manifest.json");
  if(File.Exists(manifest))z.CreateEntryFromFile(manifest,"project-manifest.json");
  var logDir=Path.Combine(source,"logs");
  if(Directory.Exists(logDir))foreach(var file in Directory.EnumerateFiles(logDir,"*.txt").Take(500))
   z.CreateEntryFromFile(file,"logs/"+Path.GetFileName(file));
  var entry=z.CreateEntry("ΣΥΝΟΨΗ.txt");
  using(var wr=new StreamWriter(entry.Open(),new UTF8Encoding(false))){
   wr.WriteLine("ΚΕΝΤΡΟ ΠΡΟΓΡΑΜΜΑΤΙΣΜΟΥ — ΑΝΑΦΟΡΑ");
   wr.WriteLine($"Έργο: {Workspace.ReadProject(id).Title}");
   wr.WriteLine($"Εκτέλεση: {checks.Id}");
   wr.WriteLine($"Πηγαίος κώδικας: {checks.SourceSha256}");
   wr.WriteLine($"Σχέδιο: {checks.PlanSha256}");
   wr.WriteLine($"Κατάσταση: {checks.Status}");
   foreach(var res in checks.Results)
    wr.WriteLine($"{res.Phase} | {res.Module} | {res.Name} | {res.Status} | {res.Category} | {res.Reason} | {res.ElapsedMs}ms");
  }
  if(!checks.Blocked){
   try{
    var request=GetReviewRequest(id);
    var review=z.CreateEntry("review_request.json");
    using var sw=new StreamWriter(review.Open(),new UTF8Encoding(false));
    sw.Write( JsonSerializer.Serialize(request,Workspace.JsonOptions));
   }catch{} // Source may have changed, so no approval request is exported.
  }
  return target;
 }

 private static async Task<TestResult> GUIStartupAsync(Step smoke,string work,CancellationToken token,Action<string>? progress)
 {
  var res=new TestResult{Phase="SMOKE",Name=smoke.Name,Module="BUILD",Required=true,Status="FAIL"};
  string file;List<string> args;
  try{(file,args)=Toolchains.Resolve(smoke.Command,smoke.Args,work);}
  catch(Exception ex){res.Status="NOT_RUN";res.Category="ENVIRONMENT";res.Reason=ex.Message;return res;}
  var psi=new ProcessStartInfo(file){WorkingDirectory=work,UseShellExecute=false,CreateNoWindow=true,
   RedirectStandardOutput=false,RedirectStandardError=false};
  foreach(var arg in args)psi.ArgumentList.Add(arg);
  Toolchains.Configure(psi);
  var sw=Stopwatch.StartNew();
  using var process=Process.Start(psi);
  if(process==null)throw new IOException("Δεν ξεκίνησε το EXE");
  try{
   await Task.Delay(TimeSpan.FromSeconds(5),token);
   if(process.HasExited){
    res.Category="EARLY_EXIT";res.Reason="Το EXE τερματίστηκε πριν το χρονικό διάστημα ελέγχου";
   }else{
    res.Status="PASS";res.Category="PASS";
    res.Reason="Το EXE εκκινήθηκε πραγματικά στα Windows και παρέμεινε σε λειτουργία 5 δευτερόλεπτα";
   }
  }catch(OperationCanceledException){
   res.Status="CANCELLED";res.Category="CANCELLED";res.Reason="Ακυρώθηκε η εκκίνηση";
  }finally{
   if(!process.HasExited){try{process.Kill(entireProcessTree:true);}catch{}}
   using var cleanup=new CancellationTokenSource(TimeSpan.FromSeconds(15));
   try{await process.WaitForExitAsync(cleanup.Token);}catch{}
   res.ElapsedMs=sw.ElapsedMilliseconds;
  }
  Emit(progress,"SMOKE: "+res.Status);
  return res;
 }

 private static TestResult CheckVisualEvidence(string root,TestResult result,Step step)
 {
  // Visual-flicker script may exit 0 even when it detects regressions.
  if(!step.Args.Any(a=>a.EndsWith("visual_flicker_windows.py",StringComparison.OrdinalIgnoreCase)))
   return result;
  string evidence=Path.Combine(root,"visual_checks","report.json");
  if(!File.Exists(evidence)){
   result.Status="FAIL";result.Category="VISUAL_EVIDENCE";
   result.Reason="Το οπτικό τεστ δεν παρήγαγε visual_checks/report.json";
   return result;
  }
  try{
   using var doc=JsonDocument.Parse(File.ReadAllText(evidence));
   var rootDoc=doc.RootElement;
   int Count(string key)=>rootDoc.TryGetProperty(key,out var v)&&v.TryGetInt32(out int n)?n:0;
   if(Count("startup_endpoint_flicker_runs")>0||
      Count("theme_endpoint_flicker_toggles")>0||
      Count("theme_clicks_confirmed")<1){
    result.Status="FAIL";result.Category="VISUAL_REGRESSION";
    result.Reason=$"Τρεμόπαιγμα κατά την εκκίνηση: {Count("startup_endpoint_flicker_runs")}, αλλαγές θέματος: {Count("theme_endpoint_flicker_toggles")}, επιβεβαιωμένα κλικ: {Count("theme_clicks_confirmed")}";
   }
  }catch(Exception ex){result.Status="FAIL";result.Category="VISUAL_EVIDENCE";result.Reason=ex.Message;}
  return result;
 }

 public static async Task<RunRecord> BuildAsync(string id,Action<string>? progress,CancellationToken token=default)
 {
  await Exclusive.WaitAsync(token);
  try{
   var plan=Workspace.ReadPlan(id);
   if(!HasApproval(id))throw new InvalidOperationException("Αναμονή ρητής έγκρισης ChatGPT για ακριβώς αυτή την εκτέλεση ελέγχων");
   await Toolchains.EnsureAsync(progress,token);
   var checks=Workspace.LastChecks(id)!;
   var record=new RunRecord{ProjectId=id,Id=Workspace.NewId(),Stage="BUILD",
    SourceSha256=checks.SourceSha256,PlanSha256=checks.PlanSha256};
   string dir=Workspace.RunDir(id,record.Id);
   Directory.CreateDirectory(dir);
   var logDir=Path.Combine(dir,"logs");
   try{
    string copy=MakeWorkspace(id,record.Id,"build");
    var step=plan.Build!;
    Emit(progress,"Κατασκευή EXE χωρίς εξωτερικά εργαλεία ή σύνδεση GitHub...");
    var built=await ExecuteAsync(step,"BUILD",copy,logDir,"build",token,progress);
    record.Results.Add(built);
    if(built.Status=="PASS"){
     var smoke=plan.Smoke!;
     var tested=await GUIStartupAsync(smoke,copy,token,progress);
     record.Results.Add(tested);
     if(tested.Status=="PASS"){
      for(int i=0;i<plan.Visual.Count;i++){
       var visual=plan.Visual[i];
       var vr=await ExecuteAsync(visual,"VISUAL",copy,logDir,"visual-"+i,token,progress);
       record.Results.Add(CheckVisualEvidence(copy,vr,visual));
      }
      if(record.Results.Any(x=>x.Phase=="VISUAL"&&x.Required&&x.Status!="PASS"))
       throw new InvalidOperationException("Ο υποχρεωτικός οπτικός έλεγχος απέτυχε — το EXE δεν εγκρίνεται");
      string relative=smoke.Command;
      if(!relative.StartsWith("./")||!relative.EndsWith(".exe",StringComparison.OrdinalIgnoreCase))
       throw new InvalidDataException("Το σχέδιο smoke δεν δείχνει παραγόμενο EXE");
      string candidate=Workspace.SafeInside(copy,relative[2..].Replace('\\','/'));
      using(var f=File.OpenRead(candidate)){
       byte[] header=new byte[2];f.ReadExactly(header);
       if(header[0]!='M'||header[1]!='Z')throw new InvalidDataException("Το προϊόν δεν είναι Windows EXE");
      }
      string outDir=Path.Combine(Workspace.DirectoryOf(id),"releases",record.Id);
      Directory.CreateDirectory(outDir);
      string name=Path.GetFileName(candidate);
      string final=Path.Combine(outDir,name);
      File.Copy(candidate,final);
      record.BinaryFile=final;
      record.BinarySha256=Workspace.HashFile(final);
      record.Results.Add(new TestResult{Module="RELEASE",Name="SHA256 παραγόμενου EXE",
       Phase="INTEGRITY",Status="PASS",Category="PASS",Reason=record.BinarySha256,Required=true});
     }
    }
    bool unchanged=record.SourceSha256==Workspace.HashTree(Workspace.SourceOf(id)) &&
     record.PlanSha256==PlanHash(id)&&HasApproval(id);
    record.Results.Add(new TestResult{Phase="INTEGRITY",Module="SECURITY",
     Name="Έγκριση και πηγαίος κώδικας αμετάβλητα",
     Status=unchanged?"PASS":"FAIL",Category=unchanged?"PASS":"INTEGRITY",Required=true,
     Reason=unchanged?"Εγκρίθηκε η ίδια ακριβώς έκδοση":"Άλλαξε το έργο κατά την κατασκευή"});
    record.Blocked=CheckStatus(record)!="PASS"||
     !record.Results.Any(x=>x.Phase=="SMOKE"&&x.Status=="PASS");
    record.Status=record.Blocked?"ΑΠΟΤΥΧΙΑ ΚΑΤΑΣΚΕΥΗΣ":"ΕΤΟΙΜΟ EXE · ΕΛΕΓΜΕΝΟ";
   }catch(Exception ex){
    record.Blocked=true;record.Status="ΑΠΟΤΥΧΙΑ ΚΑΤΑΣΚΕΥΗΣ";
    record.Results.Add(new TestResult{Module="ENGINE",Phase="BUILD",Status="FAIL",Category="ENGINE",
     Name="Σφάλμα κατασκευής",Reason=ex.Message,Required=true});
   }finally{
    if(record.Blocked&&record.BinaryFile.Length>0) {
     try{File.Delete(record.BinaryFile);}catch{}
     record.BinaryFile="";record.BinarySha256="";
    }
    record.Finished=DateTime.UtcNow;Workspace.SaveRun(record);
   }
   return record;
  }finally{Exclusive.Release();}
 }
}
