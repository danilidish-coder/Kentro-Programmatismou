using System;
using System.IO;
using System.IO.Compression;
using System.Linq;
using System.Security.Cryptography;
using System.Text;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using System.Windows.Media.Imaging;

namespace KentroOffline;

// Tests invoked only by the GitHub Windows runner using --ci-selftest.
// Production cannot generate review approvals automatically.
public static class Qa
{
 private static Action<string>? _log;
 private static void Require(bool condition,string reason)
 {
  if(!condition)throw new InvalidOperationException("CI FAILED: "+reason);
  _log?.Invoke("PASS: "+reason);
 }

 // Test the actual WPF visual tree for ALL four Greek tab captions.
 // A screenshot file alone never proves legibility: contrast and measured
 // layout must pass for selected AND unselected headers.
 private static double Channel(byte value)
 {
  double x=value/255.0;
  return x<=0.04045?x/12.92:Math.Pow((x+0.055)/1.055,2.4);
 }
 private static double Luminance(Color c)=>
  0.2126*Channel(c.R)+0.7152*Channel(c.G)+0.0722*Channel(c.B);
 private static double Contrast(Color a,Color b)
 {
  double x=Luminance(a),y=Luminance(b);
  return (Math.Max(x,y)+0.05)/(Math.Min(x,y)+0.05);
 }
 private static void VerifyGreekTabHeaders(MainWindow window)
 {
  var tabs=window.Tabs;
  Require(tabs.Items.Count==4,"Exactly four Greek navigation tabs present");
  string[] names={"ΚΩΔΙΚΑΣ / ΑΡΧΕΙΑ","ΔΟΚΙΜΕΣ / ΕΛΕΓΧΟΙ",
      "ΕΚΔΟΣΕΙΣ / ΕΠΑΝΑΦΟΡΑ","ΒΟΗΘΕΙΑ"};
  int checkedHeaders=0;
  int old=tabs.SelectedIndex;
  for(int selected=0;selected<names.Length;selected++)
  {
   tabs.SelectedIndex=selected;
   tabs.UpdateLayout();
   for(int index=0;index<names.Length;index++)
   {
    if(tabs.Items[index] is not TabItem item)
     throw new InvalidOperationException("Tab item not rendered");
    item.ApplyTemplate();
    if(item.Template.FindName("TabHeaderText",item) is not TextBlock label)
     throw new InvalidOperationException("Greek tab label template missing");
    if(item.Template.FindName("TabHeaderBorder",item) is not Border border)
     throw new InvalidOperationException("Greek tab border template missing");
    if(label.Text!=names[index]||label.ActualWidth<10||label.ActualHeight<10)
     throw new InvalidOperationException("Greek tab label missing/clipped: "+names[index]);
    if(label.Foreground is not SolidColorBrush foreground ||
       border.Background is not SolidColorBrush background)
     throw new InvalidOperationException("Tab header has undefined colors");
    double ratio=Contrast(foreground.Color,background.Color);
    if(ratio<4.5)
     throw new InvalidOperationException($"Tab {names[index]} (selected={selected==index}): contrast {ratio:F2}:1 below 4.5");
    checkedHeaders++;
   }
  }
  tabs.SelectedIndex=old;
  tabs.UpdateLayout();
  Require(checkedHeaders==16,
   "All four Greek tab captions visible and at least 4.5:1 contrast in every selected/unselected state");
 }

 public static async Task RunAsync()
 {
  Directory.CreateDirectory(Workspace.Root);
  string report=Path.Combine(Path.GetTempPath(),"kentro-v2-ci-results.log");
  using var writer=new StreamWriter(report,false,new UTF8Encoding(false)){AutoFlush=true};
  void Log(string line){writer.WriteLine(line);Console.WriteLine(line);}
  _log=Log;
  Log("KENTRO V2 REAL WINDOWS SELF TEST · "+DateTime.UtcNow.ToString("o"));
  await Toolchains.EnsureAsync(Log);
  string diag=await Toolchains.DiagnosticAsync();
  Log(diag);
  Require(diag.Contains("PySide6 + PyInstaller + Reports OK"),"Bundled Python dependencies actually import on Windows");
  Require(diag.Contains("dotnet: OK"),"Bundled .NET SDK executes offline");
  Require(diag.Contains("go: OK"),"Bundled Go compiler executes offline");
  Require(diag.Contains("node: OK"),"Bundled Node executes offline");

  var project=Workspace.CreateBlank("QA_Synthetic_"+Workspace.NewId()[..6]);
  Require(Workspace.ReadPlan(project.Id).Tests.Count>0,"New project has valid runnable plan");
  string source=Workspace.SourceOf(project.Id);
  var plan=Workspace.ReadPlan(project.Id);
  plan.Visual.Add(new Step{
   Name="Synthetic GUI visual gate",Module="VISUAL",
   Command="python",Args=new(){"-c","print('VISUAL_ACCEPTED')"},
   TimeoutSeconds=60,ExpectContains="VISUAL_ACCEPTED"
  });
  plan.Stress.Add(new Stress{
   Name="Parallel isolated stress",Module="ISOLATION",Kind="parallel",
   Command="python",Args=new(){"tests.py"},Repeats=6,Parallel=3,TimeoutSeconds=60});
  Workspace.SaveJson(Path.Combine(source,"project-manifest.json"),plan);
  string initial=Workspace.HashTree(source);
  var firstSnap=Workspace.SaveSnapshot(project.Id,"QA original project");

  var window=new MainWindow();
  window.Show();window.ReloadForQa(project.Id);window.UpdateLayout();
  Require(window.IsProjectDisplayedForQa,"Actual WPF main window binds files and plan");
  VerifyGreekTabHeaders(window);
  Require(window.VisiblePlansForQa>=4,"Visible project has actual tests, stress and build stages");
  try {
   int width=1380,height=840;
   var bmp=new RenderTargetBitmap(width,height,96,96,PixelFormats.Pbgra32);
   bmp.Render(window);
   string screenshot=Path.Combine(Path.GetTempPath(),"kentro-v2-wpf.png");
   var enc=new PngBitmapEncoder();enc.Frames.Add(BitmapFrame.Create(bmp));
   using var file=File.Create(screenshot);enc.Save(file);
   Require(new FileInfo(screenshot).Length>10000,"Native WPF screenshot captured");
  }finally{window.Close();}

  bool unauthorized=false;
  try{await Pipeline.BuildAsync(project.Id,Log);}
  catch(InvalidOperationException){unauthorized=true;}
  Require(unauthorized,"No build without real tests and reviewer decision");

  var checks=await Pipeline.ChecksAsync(project.Id,Log);
  Require(!checks.Blocked,"Synthetic independent unit, sequential, parallel, integrity all PASS");
  Require(checks.Results.Count(x=>x.Phase=="STRESS"&&x.Status=="PASS")==2,
   "Parallel and sequential stress actually executed");
  Require(checks.Results.Any(x=>x.Phase=="INTEGRITY"&&x.Status=="PASS"),
   "Tests prove original source unchanged");

  string reportZip=Path.Combine(Path.GetTempPath(),"kentro-v2-full-check-report.zip");
  Pipeline.ExportReport(project.Id,reportZip);
  using(var z=ZipFile.OpenRead(reportZip)){
   Require(z.GetEntry("results.json")!=null&&z.GetEntry("review_request.json")!=null,
    "Report contains real results and external reviewer request");
   Require(z.Entries.Any(x=>x.FullName.StartsWith("logs/")),
    "Report preserves actual test and stress logs");
  }
  unauthorized=false;
  try{await Pipeline.BuildAsync(project.Id,Log);}
  catch(InvalidOperationException){unauthorized=true;}
  Require(unauthorized,"PASS tests still cannot build without ChatGPT review");

  var request=Pipeline.GetReviewRequest(project.Id);
  var bad=new ReviewDecision{
   ProjectId=request.ProjectId,RunId=request.RunId,
   SourceSha256=request.SourceSha256,PlanSha256=request.PlanSha256,
   ResultsSha256=new string('a',64),Decision="APPROVED_FOR_BUILD",
   Reviewer="ChatGPT",Note="CI intentionally mismatched hash",
  };
  string badFile=Path.Combine(Path.GetTempPath(),"kentro-ci-bad-review.json");
  Workspace.SaveJson(badFile,bad);
  bool rejected=false;
  try{Pipeline.ImportApproval(project.Id,badFile);}
  catch(InvalidDataException){rejected=true;}
  Require(rejected&&!Pipeline.HasApproval(project.Id),"Mismatched review rejected by engine");

  // This receipt is only for the isolated synthetic CI fixture.
  var good=new ReviewDecision{
   ProjectId=request.ProjectId,RunId=request.RunId,
   SourceSha256=request.SourceSha256,PlanSha256=request.PlanSha256,
   ResultsSha256=request.ResultsSha256,Decision="APPROVED_FOR_BUILD",
   Reviewer="ChatGPT",Note="CI synthetic fixture tested in a disposable environment"
  };
  string goodFile=Path.Combine(Path.GetTempPath(),"kentro-ci-synthetic-review.json");
  Workspace.SaveJson(goodFile,good);
  Pipeline.ImportApproval(project.Id,goodFile);
  Require(Pipeline.HasApproval(project.Id),"Exact synthetic reviewer decision enables only matching build");
  var binary=await Pipeline.BuildAsync(project.Id,Log);
  Require(!binary.Blocked&&File.Exists(binary.BinaryFile),
   "Real PyInstaller onefile EXE built with BUNDLED runtime on Windows");
  Require(binary.Results.Any(x=>x.Phase=="SMOKE"&&x.Status=="PASS"),
   "Real desktop GUI executable launched and remained open");
  Require(binary.Results.Any(x=>x.Phase=="VISUAL"&&x.Status=="PASS"),
   "Mandatory post-build visual test actually executes before EXE approval");
  Require(binary.BinarySha256==Workspace.HashFile(binary.BinaryFile),"Built binary SHA256 matches");

  // Verify immutable local version history and safe pre-restore backup.
  string fileToModify=Path.Combine(source,"tests.py");
  File.AppendAllText(fileToModify,"\nprint('new code version')\n");
  Require(Workspace.Diff(project.Id,firstSnap.Id).Contains("tests.py"),
   "Local history detects actual file changes");
  Require(!Pipeline.HasApproval(project.Id),"Source edit invalidates existing build approval");
  int before=Workspace.Snapshots(project.Id).Count;
  Workspace.RestoreSnapshot(project.Id,firstSnap.Id);
  Require(Workspace.Snapshots(project.Id).Count>=before+2,
   "Restore kept backup and recorded new version");
  Require(Workspace.HashTree(source)==initial,
   "Restore returned exactly to versioned and SHA-verified source");
  Require(!Pipeline.HasApproval(project.Id),"Restoring code cannot reuse old approval");

  // Negative ZIP-slip test: never create files outside project workspace.
  string malicious=Path.Combine(Path.GetTempPath(),"kentro-zip-slip.zip");
  using(var a=ZipFile.Open(malicious,ZipArchiveMode.Create)){
   using var sw=new StreamWriter(a.CreateEntry("../escaped.txt").Open());
   sw.Write("malicious");
  }
  bool zipRejected=false;
  try{Workspace.ImportZip(malicious);}
  catch(InvalidDataException){zipRejected=true;}
  Require(zipRejected,"Unsafe ZIP traversal is rejected");

  // Directly exercise the real import path with a valid, explicit test plan.
  string sourceZip=Path.Combine(Path.GetTempPath(),"kentro-valid-source-qa.zip");
  if(File.Exists(sourceZip))File.Delete(sourceZip);
  ZipFile.CreateFromDirectory(source,sourceZip);
  var imported=Workspace.ImportZip(sourceZip);
  Require(Workspace.ReadPlan(imported.Id).Stress.Count==2,
   "ZIP import preserved original assistant-authored manifest");
  Require(Workspace.ReadProject(imported.Id).Name==project.Name,
   "Project name and version parsed from real ZIP");

  Log("ALL V2 WINDOWS TESTS PASSED");
  File.WriteAllText(Path.Combine(Path.GetTempPath(),"kentro-v2-ci-success.txt"),
   "PASS\nEXE="+binary.BinaryFile+"\nSHA256="+binary.BinarySha256+"\n"+DateTime.UtcNow.ToString("o"));
 }
}
