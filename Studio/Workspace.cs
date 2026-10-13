using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Globalization;
using System.IO;
using System.IO.Compression;
using System.Linq;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace KentroOffline;

public sealed class Step
{
 [JsonPropertyName("name")] public string Name {get;set;}="";
 [JsonPropertyName("module")] public string Module {get;set;}="GENERAL";
 [JsonPropertyName("command")] public string Command {get;set;}="";
 [JsonPropertyName("args")] public List<string> Args {get;set;}=new();
 [JsonPropertyName("timeout_seconds")] public int TimeoutSeconds {get;set;}=90;
 [JsonPropertyName("expect_contains")] public string ExpectContains {get;set;}="";
 [JsonPropertyName("required")] public bool Required {get;set;}=true;
}
public sealed class Stress:Step
{
 [JsonPropertyName("kind")] public string Kind {get;set;}="parallel";
 [JsonPropertyName("parallel")] public int Parallel {get;set;}=1;
 [JsonPropertyName("repeats")] public int Repeats {get;set;}=4;
 [JsonPropertyName("health")] public Step? Health {get;set;}
}
public sealed class Plan
{
 [JsonPropertyName("name")] public string Name {get;set;}="";
 [JsonPropertyName("version")] public string Version {get;set;}="";
 [JsonPropertyName("language")] public string Language {get;set;}="";
 [JsonPropertyName("workdir")] public string Workdir {get;set;}=".";
 [JsonPropertyName("tests")] public List<Step> Tests {get;set;}=new();
 [JsonPropertyName("stress")] public List<Stress> Stress {get;set;}=new();
 [JsonPropertyName("build")] public Step? Build {get;set;}
 [JsonPropertyName("smoke")] public Step? Smoke {get;set;}
 [JsonPropertyName("visual")] public List<Step> Visual {get;set;}=new();
}
public sealed class Project
{
 public string Id {get;set;}="";
 public string Name {get;set;}="";
 public string Version {get;set;}="";
 public string Language {get;set;}="";
 public DateTime Created {get;set;}=DateTime.UtcNow;
 public string Title=>$"{Name} · {Version}";
}
public sealed class Snapshot
{
 public string Id {get;set;}="";
 public string Parent {get;set;}="";
 public string Message {get;set;}="";
 public string Branch {get;set;}="main";
 public DateTime Created {get;set;}=DateTime.UtcNow;
 public string SourceSha256 {get;set;}="";
 public override string ToString()=>$"{Created.ToLocalTime():dd/MM HH:mm} · {Branch} · {Message} [{Id[..Math.Min(8,Id.Length)]}]";
}
public sealed class TestResult
{
 public string Module {get;set;}="";
 public string Name {get;set;}="";
 public string Phase {get;set;}="";
 public string Status {get;set;}="";
 public string Category {get;set;}="";
 public string Reason {get;set;}="";
 public long ElapsedMs {get;set;}
 public string LogFile {get;set;}="";
 public bool Required {get;set;}=true;
}
public sealed class RunRecord
{
 public string Id {get;set;}="";
 public string ProjectId {get;set;}="";
 public string Stage {get;set;}="";
 public DateTime Started {get;set;}=DateTime.UtcNow;
 public DateTime Finished {get;set;}
 public string SourceSha256 {get;set;}="";
 public string PlanSha256 {get;set;}="";
 public bool Blocked {get;set;}=true;
 public string Status {get;set;}="";
 public string BinarySha256 {get;set;}="";
 public string BinaryFile {get;set;}="";
 public List<TestResult> Results {get;set;}=new();
 public override string ToString()=>$"{Started.ToLocalTime():dd/MM HH:mm} · {Stage} · {Status} · {Results.Count(x=>x.Status=="PASS")}/{Results.Count}";
}
public sealed class ReviewRequest
{
 [JsonPropertyName("schema")] public string Schema {get;set;}="kentro-review-v1";
 [JsonPropertyName("project_id")] public string ProjectId {get;set;}="";
 [JsonPropertyName("run_id")] public string RunId {get;set;}="";
 [JsonPropertyName("source_sha256")] public string SourceSha256 {get;set;}="";
 [JsonPropertyName("plan_sha256")] public string PlanSha256 {get;set;}="";
 [JsonPropertyName("results_sha256")] public string ResultsSha256 {get;set;}="";
}
public sealed class ReviewDecision:ReviewRequest
{
 [JsonPropertyName("decision")] public string Decision {get;set;}="";
 [JsonPropertyName("reviewer")] public string Reviewer {get;set;}="";
 [JsonPropertyName("note")] public string Note {get;set;}="";
}
public static class Workspace
{
 public static readonly string Root=Path.Combine(
  Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"KentroOfflineStudioV2");
 public static readonly JsonSerializerOptions JsonOptions=new(){WriteIndented=true,PropertyNameCaseInsensitive=true};
 public static readonly object FileLock=new();
 public static string NewId()=>Guid.NewGuid().ToString("N");
 public static string DirectoryOf(string id)
 {
  if(!IsId(id))throw new InvalidOperationException("Μη έγκυρο αναγνωριστικό έργου");
  return Path.Combine(Root,"projects",id);
 }
 public static string SourceOf(string id)=>Path.Combine(DirectoryOf(id),"source");
 public static string RunsOf(string id)=>Path.Combine(DirectoryOf(id),"runs");
 public static string HistoryOf(string id)=>Path.Combine(DirectoryOf(id),"versions");
 public static bool IsId(string id)=>id.Length==32&&id.All(c=>char.IsAsciiHexDigit(c));
 public static string SafeInside(string root,string relative)
 {
  if(string.IsNullOrWhiteSpace(relative)||Path.IsPathRooted(relative)||relative.Contains(':')||
     relative.Contains('\0')||relative.Contains('\\')||relative.StartsWith('/')||
     relative.Split('/').Any(p=>p==".."||p=="."||p.Length==0))
   throw new InvalidDataException("Μη ασφαλής διαδρομή αρχείου");
  string baseRoot=Path.GetFullPath(root);
  string candidate=Path.GetFullPath(Path.Combine(baseRoot,relative.Replace('/',Path.DirectorySeparatorChar)));
  if(!candidate.StartsWith(baseRoot+Path.DirectorySeparatorChar,StringComparison.OrdinalIgnoreCase))
   throw new InvalidDataException("Το αρχείο βγαίνει έξω από τον φάκελο έργου");
  return candidate;
 }
 public static string HashFile(string file)
 {
  using var f=File.OpenRead(file);
  return Convert.ToHexString(SHA256.HashData(f)).ToLowerInvariant();
 }
 public static string HashBytes(byte[] bytes)=>Convert.ToHexString(SHA256.HashData(bytes)).ToLowerInvariant();
 public static string HashTree(string root)
 {
  using var sha=SHA256.Create();
  using var output=new MemoryStream();
  foreach(var file in Directory.EnumerateFiles(root,"*",SearchOption.AllDirectories)
   .OrderBy(f=>Path.GetRelativePath(root,f),StringComparer.Ordinal))
  {
   string relative=Path.GetRelativePath(root,file).Replace('\\','/');
   if(relative.StartsWith(".kentro/",StringComparison.OrdinalIgnoreCase))continue;
   if((File.GetAttributes(file)&FileAttributes.ReparsePoint)!=0)
    throw new InvalidDataException("Δεν επιτρέπονται συνδέσεις αρχείων στο έργο");
   byte[] name=Encoding.UTF8.GetBytes(relative);
   byte[] dataHash=SHA256.HashData(File.ReadAllBytes(file));
   output.Write(BitConverter.GetBytes(name.Length));
   output.Write(name);
   output.Write(dataHash);
  }
  output.Position=0;
  return Convert.ToHexString(sha.ComputeHash(output)).ToLowerInvariant();
 }
 public static void SaveJson<T>(string path,T obj)
 {
  lock(FileLock){
   Directory.CreateDirectory(Path.GetDirectoryName(path)!);
   string tmp=path+"."+NewId()+".tmp";
   File.WriteAllText(tmp,JsonSerializer.Serialize(obj,JsonOptions),new UTF8Encoding(false));
   File.Move(tmp,path,true);
  }
 }
 public static T LoadJson<T>(string path)
 {
  return JsonSerializer.Deserialize<T>(File.ReadAllText(path,Encoding.UTF8),JsonOptions)
   ??throw new InvalidDataException("Αδυναμία ανάγνωσης αρχείου JSON");
 }
 public static IEnumerable<Project> Projects()
 {
  string dir=Path.Combine(Root,"projects");
  if(!Directory.Exists(dir))return Array.Empty<Project>();
  var projects=new List<Project>();
  foreach(var folder in Directory.EnumerateDirectories(dir)){
   var file=Path.Combine(folder,"project.json");
   try{var p=LoadJson<Project>(file);if(IsId(p.Id)&&DirectoryOf(p.Id)==folder)projects.Add(p);}catch{}
  }
  return projects.OrderBy(x=>x.Name,StringComparer.CurrentCultureIgnoreCase);
 }
 public static Project ReadProject(string id)=>LoadJson<Project>(Path.Combine(DirectoryOf(id),"project.json"));
 public static Plan ReadPlan(string id)
 {
  var file=Path.Combine(SourceOf(id),"project-manifest.json");
  if(!File.Exists(file))throw new InvalidOperationException("Απαιτείται project-manifest.json συνταγμένο για το συγκεκριμένο πρόγραμμα");
  var plan=LoadJson<Plan>(file);
  ValidatePlan(plan);
  return plan;
 }
 public static void ValidatePlan(Plan plan)
 {
  if(plan.Name.Length>180||plan.Version.Length>90||plan.Tests.Count==0||
     plan.Tests.Count>150||plan.Stress.Count>30||plan.Visual.Count>20)
   throw new InvalidDataException("Ελλιπές ή υπερβολικά μεγάλο σχέδιο ελέγχων");
  if(plan.Workdir!=""&&plan.Workdir!=".") _=SafeInside(Root,plan.Workdir);
  foreach(var step in plan.Tests.Concat(plan.Visual).Concat(new[]{plan.Build,plan.Smoke}.OfType<Step>()))
   ValidateStep(step);
  foreach(var stress in plan.Stress){
   ValidateStep(stress);
   if(stress.Kind!="parallel"&&stress.Kind!="sequential"&&stress.Kind!="restart")
    throw new InvalidDataException($"Stress {stress.Name}: μη αναγνωρισμένο είδος '{stress.Kind}'");
   if(stress.Repeats<1||stress.Repeats>50||stress.Parallel<1||stress.Parallel>12||
      stress.TimeoutSeconds>(stress.Kind=="sequential"?900:120))
    throw new InvalidDataException($"Stress {stress.Name}: μη έγκυρες επαναλήψεις, παραλληλία ή χρόνος");
   if((stress.Kind=="sequential"||stress.Kind=="restart")&&stress.Parallel!=1)
    throw new InvalidDataException($"Stress {stress.Name}: απαιτείται parallel=1");
   if(stress.Kind=="restart"){if(stress.Health is null)throw new InvalidDataException($"Stress {stress.Name}: λείπει υποχρεωτικό health check");ValidateStep(stress.Health);}
  }
  if(plan.Build is null||plan.Smoke is null)
   throw new InvalidDataException("Το σχέδιο πρέπει να ορίζει κατασκευή EXE και πραγματική εκκίνηση");
 }
 public static void ValidateStep(Step step)
 {
  if(string.IsNullOrWhiteSpace(step.Name)||step.Name.Length>180||string.IsNullOrWhiteSpace(step.Command)||
   step.TimeoutSeconds<1||step.TimeoutSeconds>1800||step.Args.Count>70||
   step.Args.Any(x=>x.Length>4096||x.Contains('\0')))
   throw new InvalidDataException($"Μη έγκυρη εντολή ή διάρκεια στο τεστ '{step.Name}'");
 }
 public static Project ImportZip(string zip)
 {
  if(!File.Exists(zip))throw new FileNotFoundException("Το ZIP δεν βρέθηκε",zip);
  string id=NewId();
  string root=DirectoryOf(id),source=SourceOf(id);
  Directory.CreateDirectory(source);
  try {
   using(var a=ZipFile.OpenRead(zip)){
    if(a.Entries.Count>15000)throw new InvalidDataException("Πάρα πολλά αρχεία στο ZIP");
    long expanded=0;
    foreach(var entry in a.Entries){
     var item=entry.FullName.Replace('\\','/');
     if(item.EndsWith('/'))continue;
     if((entry.ExternalAttributes>>16&0xF000)==0xA000)
      throw new InvalidDataException("Το ZIP περιέχει μη επιτρεπτή συμβολική σύνδεση");
     expanded=checked(expanded+entry.Length);
     if(expanded>512L*1024*1024)throw new InvalidDataException("Το ZIP υπερβαίνει 512 MB αποσυμπιεσμένων αρχείων");
     if(entry.Length>256L*1024*1024)throw new InvalidDataException("Υπερβολικά μεγάλο αρχείο");
     string target=SafeInside(source,item);
     Directory.CreateDirectory(Path.GetDirectoryName(target)!);
     entry.ExtractToFile(target);
    }
   }
   Plan plan=ReadPlan(id);
   var p=new Project{Id=id,Name=plan.Name,Version=plan.Version,Language=plan.Language};
   SaveJson(Path.Combine(root,"project.json"),p);
   SaveSnapshot(p.Id,"Αρχική εισαγωγή πηγαίου κώδικα");
   return p;
  }catch{try{Directory.Delete(root,true);}catch{}throw;}
 }
 public static Project CreateBlank(string title)
 {
  if(title.Length<2||title.Length>100)throw new ArgumentException("Απαιτείται όνομα 2 έως 100 χαρακτήρων");
  var id=NewId();
  Directory.CreateDirectory(SourceOf(id));
  var p=new Project{Id=id,Name=title,Version="0.1.0",Language="Python"};
  File.WriteAllText(Path.Combine(SourceOf(id),"main.py"),
    "from PySide6.QtWidgets import QApplication, QLabel\nimport sys\napp=QApplication(sys.argv)\nw=QLabel('Το πρώτο μου πρόγραμμα')\nw.resize(420,180)\nw.show()\nsys.exit(app.exec())\n",new UTF8Encoding(false));
  File.WriteAllText(Path.Combine(SourceOf(id),"tests.py"),
    "from pathlib import Path\nassert Path('main.py').exists()\nprint('OK: project structure')\n",new UTF8Encoding(false));
  var plan=new Plan{
   Name=title,Version=p.Version,Language="Python",
   Tests=new(){new Step{Name="Basic regression",Module="GENERAL",Command="python",Args=new(){"tests.py"},TimeoutSeconds=120}},
   Stress=new(){new Stress{Name="Sequential smoke stress",Module="GENERAL",Command="python",Args=new(){"tests.py"},Kind="sequential",Repeats=4,Parallel=1,TimeoutSeconds=120}},
   Build=new Step{Name="PyInstaller single EXE",Command="python",Args=new(){"-m","PyInstaller","--noconfirm","--clean","--onefile","--windowed","--name","App","main.py"},TimeoutSeconds=900},
   Smoke=new Step{Name="Windows GUI startup",Command="./dist/App.exe",TimeoutSeconds=25}
  };
  SaveJson(Path.Combine(SourceOf(id),"project-manifest.json"),plan);
  SaveJson(Path.Combine(DirectoryOf(id),"project.json"),p);
  SaveSnapshot(id,"Νέο τοπικό πρόγραμμα");
  return p;
 }
 public static IReadOnlyList<string> Files(string id)=>
  Directory.EnumerateFiles(SourceOf(id),"*",SearchOption.AllDirectories)
   .Select(f=>Path.GetRelativePath(SourceOf(id),f).Replace('\\','/'))
   .OrderBy(f=>f,StringComparer.OrdinalIgnoreCase).ToArray();
 public static void CopyTree(string src,string dst)
 {
  Directory.CreateDirectory(dst);
  foreach(var file in Directory.EnumerateFiles(src,"*",SearchOption.AllDirectories)){
   if((File.GetAttributes(file)&FileAttributes.ReparsePoint)!=0)
    throw new InvalidDataException("Δεν επιτρέπονται συμβολικοί σύνδεσμοι");
   string rel=Path.GetRelativePath(src,file).Replace('\\','/');
   string target=SafeInside(dst,rel);
   Directory.CreateDirectory(Path.GetDirectoryName(target)!);
   File.Copy(file,target,true);
  }
 }
 public static Snapshot SaveSnapshot(string id,string message,string branch="main")
 {
  if(message.Length>250)throw new InvalidDataException("Υπερβολικά μεγάλο σχόλιο έκδοσης");
  string history=HistoryOf(id);
  Directory.CreateDirectory(history);
  var prev=Snapshots(id).FirstOrDefault();
  var snap=new Snapshot{Id=NewId(),Message=message,Branch=branch,Parent=prev?.Id??"",SourceSha256=HashTree(SourceOf(id))};
  string folder=Path.Combine(history,snap.Id);
  CopyTree(SourceOf(id),Path.Combine(folder,"source"));
  SaveJson(Path.Combine(folder,"snapshot.json"),snap);
  return snap;
 }
 public static IReadOnlyList<Snapshot> Snapshots(string id)
 {
  var folder=HistoryOf(id);
  if(!Directory.Exists(folder))return Array.Empty<Snapshot>();
  var result=new List<Snapshot>();
  foreach(string file in Directory.EnumerateFiles(folder,"snapshot.json",SearchOption.AllDirectories)){
   try{var snap=LoadJson<Snapshot>(file);if(IsId(snap.Id))result.Add(snap);}catch{}
  }
  return result.OrderByDescending(s=>s.Created).ToList();
 }
 public static string Diff(string id,string snapshotId)
 {
  if(!IsId(snapshotId))throw new InvalidDataException("Ακατάλληλη έκδοση");
  string other=Path.Combine(HistoryOf(id),snapshotId,"source");
  if(!Directory.Exists(other))throw new DirectoryNotFoundException("Η έκδοση λείπει");
  string live=SourceOf(id);
  var oldFiles=Directory.EnumerateFiles(other,"*",SearchOption.AllDirectories)
   .ToDictionary(x=>Path.GetRelativePath(other,x).Replace('\\','/'),x=>HashFile(x),StringComparer.OrdinalIgnoreCase);
  var newFiles=Directory.EnumerateFiles(live,"*",SearchOption.AllDirectories)
   .ToDictionary(x=>Path.GetRelativePath(live,x).Replace('\\','/'),x=>HashFile(x),StringComparer.OrdinalIgnoreCase);
  var lines=new List<string>();
  foreach(string key in oldFiles.Keys.Union(newFiles.Keys,StringComparer.OrdinalIgnoreCase).OrderBy(x=>x)){
   if(!oldFiles.ContainsKey(key))lines.Add("+ ΝΕΟ: "+key);
   else if(!newFiles.ContainsKey(key))lines.Add("- ΑΦΑΙΡΕΘΗΚΕ: "+key);
   else if(oldFiles[key]!=newFiles[key])lines.Add("~ ΤΡΟΠΟΠΟΙΗΘΗΚΕ: "+key);
  }
  return lines.Count==0?"Δεν υπάρχουν αλλαγές από αυτή την έκδοση.":string.Join(Environment.NewLine,lines);
 }
 public static void RestoreSnapshot(string id,string snapshotId)
 {
  if(!IsId(snapshotId))throw new InvalidDataException("Άκυρη έκδοση");
  string folder=Path.Combine(HistoryOf(id),snapshotId,"source");
  if(!Directory.Exists(folder))throw new DirectoryNotFoundException();
  SaveSnapshot(id,"Αυτόματο αντίγραφο ΠΡΙΝ από επαναφορά");
  var live=SourceOf(id);
  var stage=Path.Combine(DirectoryOf(id),"restore-"+NewId());
  CopyTree(folder,stage);
  if(HashTree(folder)!=HashTree(stage))throw new IOException("Αποτυχία επαλήθευσης αντίγραφου");
  string old=live+".backup-"+NewId();
  Directory.Move(live,old);
  try{Directory.Move(stage,live);}
  catch{Directory.Move(old,live);throw;}
  // Retain the rollback backup rather than silently discarding user work.
  string preserved=Path.Combine(DirectoryOf(id),"rollback",Path.GetFileName(old));
  Directory.CreateDirectory(Path.GetDirectoryName(preserved)!);
  Directory.Move(old,preserved);
  SaveSnapshot(id,"Επαναφορά παλαιότερης έκδοσης "+snapshotId[..8]);
 }
 public static string RunDir(string id,string runId)
 {
  if(!IsId(runId))throw new InvalidDataException("Άκυρο αναγνωριστικό εκτέλεσης");
  return Path.Combine(RunsOf(id),runId);
 }
 public static IReadOnlyList<RunRecord> Runs(string id)
 {
  var dir=RunsOf(id);
  if(!Directory.Exists(dir))return Array.Empty<RunRecord>();
  var runs=new List<RunRecord>();
  foreach(var folder in Directory.EnumerateDirectories(dir)){
   try {
    var r=LoadJson<RunRecord>(Path.Combine(folder,"results.json"));
    if(r.ProjectId==id&&r.Id==Path.GetFileName(folder))runs.Add(r);
   }catch{}
  }
  return runs.OrderByDescending(r=>r.Started).ToArray();
 }
 public static RunRecord? LastChecks(string id)=>Runs(id).FirstOrDefault(x=>x.Stage=="CHECKS");
 public static void SaveRun(RunRecord run)=>SaveJson(Path.Combine(RunDir(run.ProjectId,run.Id),"results.json"),run);
}
