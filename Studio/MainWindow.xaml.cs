using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Linq;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using Microsoft.Win32;

namespace KentroOffline;

public partial class MainWindow:Window
{
 private Project? _selected;
 private string? _selectedFile;
 private CancellationTokenSource? _cancel;
 private bool _busy;
 private bool _refreshing;

 public MainWindow()
 {
  InitializeComponent();
  try{ReloadProjects();StatusLabel.Text=Toolchains.IsReady
   ?"Όλα τα εργαλεία είναι ήδη διαθέσιμα τοπικά · Δεν χρειάζεται GitHub"
   :"Τα εργαλεία Python / Go / .NET / Node είναι ενσωματωμένα και προετοιμάζονται κατά τον πρώτο έλεγχο";}
  catch(Exception e){Log("Σφάλμα κατά την αρχική φόρτωση: "+e.Message);}
  RefreshButtons();
 }
 private void Log(string text)
 {
  if(!Dispatcher.CheckAccess()){Dispatcher.Invoke(()=>Log(text));return;}
  LogBox.AppendText($"[{DateTime.Now:HH:mm:ss}] {text}\n");
  if(LogBox.Text.Length>180000)LogBox.Text=LogBox.Text[^150000..];
  LogBox.ScrollToEnd();
  StatusLabel.Text=text;
 }
 private void ReloadProjects(string? selectId=null)
 {
  _refreshing=true;
  string? prev=selectId??_selected?.Id;
  var projects=Workspace.Projects().ToArray();
  ProjectList.ItemsSource=projects;
  var chosen=projects.FirstOrDefault(x=>x.Id==prev);
  _refreshing=false;
  if(chosen!=null)ProjectList.SelectedItem=chosen;
  else if(projects.Length>0)ProjectList.SelectedIndex=0;
  else{_selected=null;ProjectTitle.Text="Δεν έχει εισαχθεί πρόγραμμα";ProjectSubtitle.Text="Εισάγετε ZIP ή δημιουργήστε πρόγραμμα.";ResetViews();}
 }
 private void ResetViews()
 {
  FileList.ItemsSource=null;
  FileNameLabel.Text="Επιλέξτε αρχείο";
  CodeEditor.Text="";
  PlanList.ItemsSource=null;
  RunList.ItemsSource=null;
  SnapshotList.ItemsSource=null;
  _selectedFile=null;
  RefreshButtons();
 }
 private void ReloadProjectData()
 {
  if(_selected==null){ResetViews();return;}
  try{
   ProjectTitle.Text=_selected.Name;
   ProjectSubtitle.Text=$"Έκδοση {_selected.Version} · {_selected.Language} · Τοπικό έργο · {Workspace.Files(_selected.Id).Count} αρχεία";
   FileList.ItemsSource=Workspace.Files(_selected.Id);
   var plan=Workspace.ReadPlan(_selected.Id);
   var steps=new List<string>();
   foreach(var t in plan.Tests)steps.Add($"TEST [{t.Module}]\n{t.Name}\n{t.Command} {string.Join(' ',t.Args)} · {t.TimeoutSeconds}s");
   foreach(var t in plan.Stress)steps.Add($"STRESS [{t.Module}]\n{t.Name} · {t.Kind}\n{t.Repeats} φορές / {t.Parallel} παράλληλες · {t.TimeoutSeconds}s");
   steps.Add("BUILD\n"+plan.Build?.Name+"\n"+plan.Build?.Command+" "+string.Join(' ',plan.Build?.Args??new List<string>()));
   steps.Add("SMOKE\n"+plan.Smoke?.Name);
   PlanList.ItemsSource=steps;
   var runs=Workspace.Runs(_selected.Id);
   RunList.ItemsSource=runs;
   SnapshotList.ItemsSource=Workspace.Snapshots(_selected.Id);
   RefreshButtons();
  }catch(Exception ex){Log("Έργο: "+ex.Message);RefreshButtons();}
 }
 private void RefreshButtons()
 {
  bool selected=_selected!=null;
  ImportButton.IsEnabled=!_busy;NewProjectButton.IsEnabled=!_busy;ReloadButton.IsEnabled=!_busy;
  TestButton.IsEnabled=selected&&!_busy;
  CodeEditor.IsReadOnly=_busy;
  FolderButton.IsEnabled=selected;
  RuntimeButton.IsEnabled=!_busy;
  SnapshotButton.IsEnabled=selected&&!_busy;
  CompareButton.IsEnabled=selected&&SnapshotList.SelectedItem!=null&&!_busy;
  RestoreButton.IsEnabled=selected&&SnapshotList.SelectedItem!=null&&!_busy;
  BuildButton.IsEnabled=selected&&!_busy&&Pipeline.HasApproval(_selected!.Id);
  ReviewButton.IsEnabled=selected&&!_busy&&Workspace.LastChecks(_selected!.Id) is {Blocked:false};
  ReportButton.IsEnabled=selected&&Workspace.LastChecks(_selected!.Id)!=null&&!_busy;
  ReleaseFolderButton.IsEnabled=selected;
  CancelButton.IsEnabled=_busy;
 }
 private void Project_Changed(object sender,SelectionChangedEventArgs e)
 {
  if(_refreshing)return;
  _selected=ProjectList.SelectedItem as Project;
  ReloadProjectData();
 }
 private void Reload_Click(object sender,RoutedEventArgs e)=>ReloadProjects();
 private void Folder_Click(object sender,RoutedEventArgs e)
 {
  if(_selected!=null)OpenFolder(Workspace.SourceOf(_selected.Id));
 }
 private static void OpenFolder(string path)
 {
  Directory.CreateDirectory(path);
  Process.Start(new ProcessStartInfo(path){UseShellExecute=true});
 }
 private void Import_Click(object sender,RoutedEventArgs e)
 {
  if(_busy)return;
  var dialog=new OpenFileDialog{Title="Επιλογή ZIP πηγαίου κώδικα",Filter="Αρχεία ZIP (*.zip)|*.zip",CheckFileExists=true};
  if(dialog.ShowDialog(this)!=true)return;
  try{
   var p=Workspace.ImportZip(dialog.FileName);
   Log($"Εισαγωγή {p.Name} · {p.Version}: επιτυχής. Δημιουργήθηκε ασφαλές ιστορικό.");
   ReloadProjects(p.Id);
  }catch(Exception ex){ShowError("Αποτυχία εισαγωγής",ex);}
 }
 private void NewProject_Click(object sender,RoutedEventArgs e)
 {
  if(_busy)return;
  string? name=Ask("Νέο πρόγραμμα","Όνομα νέου προγράμματος");
  if(string.IsNullOrWhiteSpace(name))return;
  try{
   var p=Workspace.CreateBlank(name.Trim());
   ReloadProjects(p.Id);
   Log("Δημιουργήθηκε νέο πρόγραμμα με αρχικό κώδικα, σχέδιο ελέγχων και ιστορικό.");
  }catch(Exception ex){ShowError("Νέο πρόγραμμα",ex);}
 }
 private string? Ask(string title,string question)
 {
  var dialog=new Window{
   Title=title,Owner=this,Width=460,Height=185,ResizeMode=ResizeMode.NoResize,
   WindowStartupLocation=WindowStartupLocation.CenterOwner,Background=System.Windows.Media.Brushes.White};
  var stack=new StackPanel{Margin=new Thickness(16)};
  stack.Children.Add(new TextBlock{Text=question,Foreground=System.Windows.Media.Brushes.Black,Margin=new Thickness(0,0,0,12)});
  var entry=new TextBox{Foreground=System.Windows.Media.Brushes.Black,Background=System.Windows.Media.Brushes.White};
  stack.Children.Add(entry);
  var ok=new Button{Content="ΕΝΤΑΞΕΙ",HorizontalAlignment=HorizontalAlignment.Right,Width=120,Margin=new Thickness(0,10,0,0)};
  ok.Click+=(_,_)=>{dialog.DialogResult=true;dialog.Close();};
  stack.Children.Add(ok);dialog.Content=stack;
  entry.Focus();return dialog.ShowDialog()==true?entry.Text:null;
 }
 private void File_Changed(object sender,SelectionChangedEventArgs e)
 {
  if(_selected==null||FileList.SelectedItem is not string rel)return;
  try{
   string full=Workspace.SafeInside(Workspace.SourceOf(_selected.Id),rel);
   if(new FileInfo(full).Length>1024*1024){
    CodeEditor.Text="Το αρχείο είναι μεγάλο ή δυαδικό. Ανοίξτε το από τον φάκελο.";
    CodeEditor.IsReadOnly=true;_selectedFile=null;return;
   }
   var bytes=File.ReadAllBytes(full);
   if(bytes.Contains((byte)0)){
    CodeEditor.Text="Δυαδικό αρχείο: δεν επιτρέπεται επεξεργασία στο πρόγραμμα.";
    CodeEditor.IsReadOnly=true;_selectedFile=null;return;
   }
   _selectedFile=rel;
   FileNameLabel.Text=rel;
   CodeEditor.IsReadOnly=_busy;CodeEditor.Text=File.ReadAllText(full,Encoding.UTF8);
  }catch(Exception ex){ShowError("Προβολή αρχείου",ex);}
 }
 private void SaveFile_Click(object sender,RoutedEventArgs e)
 {
  if(_busy||_selected==null||_selectedFile==null)return;
  try{
   string full=Workspace.SafeInside(Workspace.SourceOf(_selected.Id),_selectedFile);
   File.WriteAllText(full,CodeEditor.Text,new UTF8Encoding(false));
   Log("Αποθηκεύτηκε "+_selectedFile+". Τυχόν παλιά έγκριση EXE ακυρώνεται αυτόματα.");
   ReloadProjectData();
  }catch(Exception ex){ShowError("Αποθήκευση αρχείου",ex);}
 }
 private void NewFile_Click(object sender,RoutedEventArgs e)
 {
  if(_busy||_selected==null)return;
  string? rel=Ask("Νέο αρχείο","Σχετική διαδρομή π.χ. src/feature.py");
  if(string.IsNullOrWhiteSpace(rel))return;
  try{
   var file=Workspace.SafeInside(Workspace.SourceOf(_selected.Id),rel.Replace('\\','/'));
   if(File.Exists(file))throw new IOException("Το αρχείο υπάρχει ήδη");
   Directory.CreateDirectory(Path.GetDirectoryName(file)!);
   File.WriteAllText(file,"",new UTF8Encoding(false));
   Log("Νέο αρχείο: "+rel);
   ReloadProjectData();
   FileList.SelectedItem=rel.Replace('\\','/');
  }catch(Exception ex){ShowError("Νέο αρχείο",ex);}
 }
 private void Snapshot_Click(object sender,RoutedEventArgs e)
 {
  if(_selected==null||_busy)return;
  string? message=Ask("Νέα έκδοση","Περιγραφή αλλαγών");
  if(string.IsNullOrWhiteSpace(message))return;
  try{Workspace.SaveSnapshot(_selected.Id,message.Trim());ReloadProjectData();Log("Αποθηκεύτηκε νέα έκδοση με SHA256.");}
  catch(Exception ex){ShowError("Έκδοση",ex);}
 }
 private void Snapshot_Changed(object sender,SelectionChangedEventArgs e)=>RefreshButtons();
 private void Compare_Click(object sender,RoutedEventArgs e)
 {
  if(_selected==null||SnapshotList.SelectedItem is not Snapshot snap)return;
  try{
   var diff=Workspace.Diff(_selected.Id,snap.Id);
   MessageBox.Show(this,diff,"Σύγκριση με "+snap.Id[..8],MessageBoxButton.OK,MessageBoxImage.Information);
  }catch(Exception ex){ShowError("Σύγκριση",ex);}
 }
 private void Restore_Click(object sender,RoutedEventArgs e)
 {
  if(_selected==null||SnapshotList.SelectedItem is not Snapshot snap||_busy)return;
  if(MessageBox.Show(this,"Θα γίνει πρώτα νέο αντίγραφο ασφαλείας και μετά επαναφορά στην επιλεγμένη έκδοση. Συνέχεια;",
    "Επιβεβαίωση επαναφοράς",MessageBoxButton.YesNo,MessageBoxImage.Warning)!=MessageBoxResult.Yes)return;
  try{Workspace.RestoreSnapshot(_selected.Id,snap.Id);ReloadProjectData();Log("Έγινε επαναφορά· διατηρήθηκε αυτόματο αντίγραφο πριν από την αλλαγή.");}
  catch(Exception ex){ShowError("Επαναφορά",ex);}
 }
 private void ReleaseFolder_Click(object sender,RoutedEventArgs e)
 {
  if(_selected!=null)OpenFolder(Path.Combine(Workspace.DirectoryOf(_selected.Id),"releases"));
 }
 private void Run_Changed(object sender,SelectionChangedEventArgs e)
 {
  if(RunList.SelectedItem is not RunRecord run)return;
  Log($"Αναφορά {run.Id[..8]}: {run.Stage} · {run.Status}");
  foreach(var x in run.Results.Where(x=>x.Status!="PASS"))
   Log($"  {x.Module}/{x.Name}: {x.Status} {x.Category} — {x.Reason}");
 }
 private async Task RunBusyAsync(string operation,Func<CancellationToken,Task> action)
 {
  if(_busy)return;
  _busy=true;_cancel=new CancellationTokenSource();
  RefreshButtons();
  try{await action(_cancel.Token);}
  catch(OperationCanceledException){Log("Ακυρώθηκε: "+operation);}
  catch(Exception ex){ShowError(operation,ex);}
  finally{_cancel?.Dispose();_cancel=null;_busy=false;ReloadProjectData();RefreshButtons();}
 }
 private async void Tests_Click(object sender,RoutedEventArgs e)
 {
  if(_selected==null)return;
  string id=_selected.Id;
  Tabs.SelectedIndex=1;
  await RunBusyAsync("Έλεγχοι",async token=>{
   var run=await Pipeline.ChecksAsync(id,Log,token);
   Log($"ΤΕΛΟΣ ΕΛΕΓΧΩΝ: {run.Status}");
   if(run.Blocked)MessageBox.Show(this,"Οι έλεγχοι δεν εγκρίθηκαν. Εξάγετε την ΑΝΑΦΟΡΑ και στείλτε την στο ChatGPT.","Ολοκλήρωση",MessageBoxButton.OK,MessageBoxImage.Warning);
   else MessageBox.Show(this,"Οι έλεγχοι πέρασαν. Εξάγετε την αναφορά και στείλτε την για ανεξάρτητη έγκριση. Το EXE παραμένει κλειδωμένο.","Αναμονή έγκρισης",MessageBoxButton.OK,MessageBoxImage.Information);
  });
 }
 private void Report_Click(object sender,RoutedEventArgs e)
 {
  if(_selected==null)return;
  var file=new SaveFileDialog{
   Title="Εξαγωγή αναφοράς ελέγχων για ChatGPT",Filter="ZIP αναφοράς (*.zip)|*.zip",
   FileName="ΑΝΑΦΟΡΑ_"+_selected.Version+"_"+DateTime.Now.ToString("yyyyMMdd_HHmm")+".zip"
  };
  if(file.ShowDialog(this)!=true)return;
  try{Pipeline.ExportReport(_selected.Id,file.FileName);Log("Εξήχθη η αναφορά: "+file.FileName);}
  catch(Exception ex){ShowError("Εξαγωγή αναφοράς",ex);}
 }
 private void Review_Click(object sender,RoutedEventArgs e)
 {
  if(_selected==null)return;
  var picker=new OpenFileDialog{Title="Αρχείο έγκρισης ChatGPT",Filter="JSON έγκρισης (*.json)|*.json",CheckFileExists=true};
  if(picker.ShowDialog(this)!=true)return;
  try{
   Pipeline.ImportApproval(_selected.Id,picker.FileName);
   Log("Εγκρίθηκε ακριβώς αυτός ο κώδικας και οι συγκεκριμένες δοκιμές.");
   RefreshButtons();
  }catch(Exception ex){ShowError("Απόρριψη έγκρισης",ex);}
 }
 private async void Build_Click(object sender,RoutedEventArgs e)
 {
  if(_selected==null)return;
  string id=_selected.Id;
  await RunBusyAsync("Κατασκευή EXE",async token=>{
   var built=await Pipeline.BuildAsync(id,Log,token);
   Log("ΤΕΛΟΣ ΚΑΤΑΣΚΕΥΗΣ: "+built.Status);
   if(built.Blocked){
    MessageBox.Show(this,"Δεν δημιουργήθηκε εγκεκριμένο EXE. Ελέγξτε τα αποτελέσματα και τα logs.","Σφάλμα κατασκευής",MessageBoxButton.OK,MessageBoxImage.Warning);
   }else{
    MessageBox.Show(this,"Δημιουργήθηκε και ελέγχθηκε το EXE:\n"+built.BinaryFile,
      "Επιτυχής κατασκευή",MessageBoxButton.OK,MessageBoxImage.Information);
   }
  });
 }
 private void Cancel_Click(object sender,RoutedEventArgs e)=>_cancel?.Cancel();
 private async void Runtime_Click(object sender,RoutedEventArgs e)
 {
  await RunBusyAsync("Έλεγχος ενσωματωμένων εργαλείων",async token=>{
   var results=await Toolchains.DiagnosticAsync(token);
   Log(results);
   MessageBox.Show(this,results,"Ενσωματωμένα εργαλεία — πραγματικός έλεγχος");
  });
 }
 private void ShowError(string title,Exception ex)
 {
  Log(title+": "+ex.Message);
  MessageBox.Show(this,ex.Message,title,MessageBoxButton.OK,MessageBoxImage.Error);
 }
 public void ReloadForQa(string id)=>ReloadProjects(id);
 public bool IsProjectDisplayedForQa=>_selected!=null&&PlanList.Items.Count>0&&FileList.Items.Count>0;
 public int VisiblePlansForQa=>PlanList.Items.Count;
}
