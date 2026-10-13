using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.IO.Compression;
using System.Linq;
using System.Reflection;
using System.Security.Cryptography;
using System.Text;
using System.Threading;
using System.Threading.Tasks;

namespace KentroOffline;

public static class Toolchains
{
 public const string BundleRevision="portable-toolchain-v2-20261013";
 public static string BaseDir=>Path.Combine(Workspace.Root,"toolchains",BundleRevision);
 public static string Python=>Path.Combine(BaseDir,"python","python.exe");
 public static string Go=>Path.Combine(BaseDir,"go","bin","go.exe");
 public static string Dotnet=>Path.Combine(BaseDir,"dotnet","dotnet.exe");
 public static string Node=>Path.Combine(BaseDir,"node","node.exe");
 private static readonly SemaphoreSlim Semaphore=new(1,1);
 public static bool IsReady=>File.Exists(Path.Combine(BaseDir,"READY.ok")) &&
  File.Exists(Python)&&File.Exists(Go)&&File.Exists(Dotnet)&&File.Exists(Node);

 public static Task EnsureAsync(Action<string>? report=null,CancellationToken cancel=default)
 {
  if(IsReady)return Task.CompletedTask;
  // Extract off the UI thread; otherwise the first click would freeze WPF.
  return Task.Run(()=>EnsureBlocking(report,cancel),cancel);
 }
 private static void EnsureBlocking(Action<string>? report,CancellationToken cancel)
 {
  Semaphore.Wait(cancel);
  try{
   if(IsReady)return;
   Directory.CreateDirectory(Path.GetDirectoryName(BaseDir)!);
   using var crossProcess=new Mutex(false,"Local\\KentroStandaloneToolchainV2");
   bool owned=false;
   try{
    while(!(owned=crossProcess.WaitOne(200)))cancel.ThrowIfCancellationRequested();
    if(IsReady)return;
    report?.Invoke("Προετοιμασία ενσωματωμένων Python, Go, .NET SDK και Node. Γίνεται μόνο την πρώτη φορά.");
    using Stream? resource=typeof(Toolchains).Assembly.GetManifestResourceStream("Kentro.Toolchains");
    if(resource==null)throw new InvalidOperationException("Το EXE δεν περιέχει τα εργαλεία ανάπτυξης. Η κατασκευή του Κέντρου είναι ελλιπής.");
    string stage=Path.Combine(Path.GetDirectoryName(BaseDir)!,BundleRevision+".unpack-"+Workspace.NewId());
    Directory.CreateDirectory(stage);
    try{
     using var archive=new ZipArchive(resource,ZipArchiveMode.Read);
     long total=0;int files=0;
     foreach(var e in archive.Entries){
      cancel.ThrowIfCancellationRequested();
      if(e.FullName.EndsWith('/'))continue;
      if(++files>100000)throw new InvalidDataException("Μη έγκυρο πακέτο εργαλείων");
      total=checked(total+e.Length);
      if(total>5L*1024*1024*1024)throw new InvalidDataException("Υπέρβαση ορίου πακέτου εργαλείων");
      string dest=Workspace.SafeInside(stage,e.FullName.Replace('\\','/'));
      Directory.CreateDirectory(Path.GetDirectoryName(dest)!);
      e.ExtractToFile(dest);
      if(files%1500==0)report?.Invoke($"Προετοιμασία εργαλείων: {files} αρχεία...");
     }
     foreach(string required in new[]{
      "python/python.exe","go/bin/go.exe","dotnet/dotnet.exe","node/node.exe"})
     if(!File.Exists(Workspace.SafeInside(stage,required)))
      throw new InvalidDataException("Λείπει αναγκαίο εργαλείο από το ενσωματωμένο πακέτο: "+required);
     File.WriteAllText(Path.Combine(stage,"READY.ok"),BundleRevision,Encoding.UTF8);
     if(Directory.Exists(BaseDir))Directory.Delete(BaseDir,true);
     Directory.Move(stage,BaseDir);
    }catch{
     try{Directory.Delete(stage,true);}catch{}
     throw;
    }
    report?.Invoke("Έτοιμα τα τοπικά εργαλεία ανάπτυξης.");
   }finally{if(owned)crossProcess.ReleaseMutex();}
  }finally{Semaphore.Release();}
 }
 public static string? Executable(string command)
 {
  return command.ToLowerInvariant() switch {
   "python" or "python3" or "py" or "pyinstaller" or "pytest"=>Python,
   "go"=>Go,
   "dotnet"=>Dotnet,
   "node"=>Node,
   _=>null
  };
 }
 public static (string File,List<string> Args) Resolve(string command,List<string> args,string workspace)
 {
  var binary=Executable(command);
  if(binary!=null){
   if(!IsReady)throw new InvalidOperationException("Τα ενσωματωμένα εργαλεία δεν έχουν προετοιμαστεί");
   if(!File.Exists(binary))throw new FileNotFoundException("Λείπει το ενσωματωμένο εργαλείο "+command,binary);
   var normalized=new List<string>(args);
   if(command.Equals("pyinstaller",StringComparison.OrdinalIgnoreCase))
    normalized.InsertRange(0,new[]{"-m","PyInstaller"});
   if(command.Equals("pytest",StringComparison.OrdinalIgnoreCase))
    normalized.InsertRange(0,new[]{"-m","pytest"});
   return (binary,normalized);
  }
  if(!command.StartsWith("./")||!command.EndsWith(".exe",StringComparison.OrdinalIgnoreCase))
   throw new InvalidOperationException("Μη επιτρεπτή εντολή. Χρησιμοποιήστε ενσωματωμένα Python, Go, .NET, Node ή σχετικό EXE από τον φάκελο έργου.");
  string path=Workspace.SafeInside(workspace,command[2..].Replace('\\','/'));
  if(!File.Exists(path))throw new FileNotFoundException("Δεν έχει παραχθεί το ζητούμενο EXE",path);
  return (path,args);
 }
 public static void Configure(ProcessStartInfo info)
 {
  string[] envKeys={"PYTHONHOME","PYTHONPATH","PYTHONUSERBASE","VIRTUAL_ENV","PYTHONSTARTUP","DOTNET_ROOT","GOROOT","GOPATH","GOTOOLCHAIN","GOPROXY","GOSUMDB","NUGET_PACKAGES","PIP_NO_INDEX","PIP_DISABLE_PIP_VERSION_CHECK"};
  foreach(var k in envKeys)info.Environment.Remove(k);
  info.Environment["PYTHONHOME"]=Path.Combine(BaseDir,"python");
  info.Environment["PYTHONNOUSERSITE"]="1";
  info.Environment["PIP_NO_INDEX"]="1";
  info.Environment["PIP_DISABLE_PIP_VERSION_CHECK"]="1";
  info.Environment["GOROOT"]=Path.Combine(BaseDir,"go");
  info.Environment["GOPATH"]=Path.Combine(Workspace.Root,"go-cache");
  info.Environment["GOTOOLCHAIN"]="local";
  info.Environment["GOPROXY"]="off";
  info.Environment["GOSUMDB"]="off";
  info.Environment["DOTNET_ROOT"]=Path.Combine(BaseDir,"dotnet");
  info.Environment["DOTNET_SKIP_FIRST_TIME_EXPERIENCE"]="1";
  info.Environment["DOTNET_CLI_TELEMETRY_OPTOUT"]="1";
  info.Environment["DOTNET_NOLOGO"]="1";
  info.Environment["NUGET_PACKAGES"]=Path.Combine(Workspace.Root,"nuget-cache");
  info.Environment["PATH"]=string.Join(Path.PathSeparator,new[]{
   Path.Combine(BaseDir,"python"),Path.Combine(BaseDir,"python","Scripts"),
   Path.Combine(BaseDir,"go","bin"),Path.Combine(BaseDir,"dotnet"),
   Path.Combine(BaseDir,"node"),Environment.GetEnvironmentVariable("PATH")??""});
 }
 public static async Task<string> DiagnosticAsync(CancellationToken cancel=default)
 {
  await EnsureAsync(null,cancel);
  var results=new List<string>();
  foreach(var (tool,args) in new[]{
   ("python",new[]{"-c","import sys,PyInstaller,PySide6,openpyxl,PIL,docx,pytest;print('Python '+sys.version.split()[0]+' / PySide6 + PyInstaller + Reports OK')"}),
   ("go",new[]{"version"}),("dotnet",new[]{"--version"}),("node",new[]{"--version"})
  }){
   var (file,a)=Resolve(tool,args.ToList(),BaseDir);
   var psi=new ProcessStartInfo(file){UseShellExecute=false,CreateNoWindow=true,
      RedirectStandardOutput=true,RedirectStandardError=true,WorkingDirectory=BaseDir};
   foreach(string arg in a)psi.ArgumentList.Add(arg);
   Configure(psi);
   using var proc=Process.Start(psi)??throw new InvalidOperationException("Αδυναμία εκκίνησης "+tool);
   string output=await proc.StandardOutput.ReadToEndAsync(cancel);
   string error=await proc.StandardError.ReadToEndAsync(cancel);
   using var cts=CancellationTokenSource.CreateLinkedTokenSource(cancel);
   cts.CancelAfter(TimeSpan.FromSeconds(60));
   try{await proc.WaitForExitAsync(cts.Token);}catch(OperationCanceledException){proc.Kill(true);throw new TimeoutException("Έλεγχος εργαλείων: υπέρβαση χρόνου "+tool);}
   results.Add(tool+": "+(proc.ExitCode==0?"OK":"ΑΠΟΤΥΧΙΑ")+" "+(proc.ExitCode==0?output:error).Trim());
   if(proc.ExitCode!=0)throw new InvalidOperationException(tool+" δεν λειτουργεί στην ενσωματωμένη διανομή:\n"+error);
  }
  return string.Join(Environment.NewLine,results);
 }
}
