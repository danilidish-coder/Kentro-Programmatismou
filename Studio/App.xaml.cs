using System;
using System.IO;
using System.Linq;
using System.Windows;
namespace KentroOffline;
public partial class App : Application
{
 protected override async void OnStartup(StartupEventArgs e)
 {
  DispatcherUnhandledException += (_,a) => {
   try {Directory.CreateDirectory(Workspace.Root);File.AppendAllText(Path.Combine(Workspace.Root,"errors.log"),DateTime.Now+" "+a.Exception+"\n");}catch{}
   MessageBox.Show("Προέκυψε σφάλμα. Δεν τροποποιήθηκαν τα έργα σας.\n"+a.Exception.Message,"Κέντρο Προγραμματισμού");
   a.Handled=true;
  };
  base.OnStartup(e);
  if(e.Args.Contains("--ci-selftest",StringComparer.OrdinalIgnoreCase))
  {
   ShutdownMode=ShutdownMode.OnExplicitShutdown;
   try { await Qa.RunAsync(); Environment.ExitCode=0; }
   catch(Exception ex){
    Console.Error.WriteLine(ex);
    try{File.WriteAllText(Path.Combine(Path.GetTempPath(),"kentro-v2-ci-failure.txt"),ex.ToString());}catch{}
    Environment.ExitCode=1;
   }
   Shutdown(Environment.ExitCode);
   return;
  }
  MainWindow=new MainWindow();
  MainWindow.Show();
 }
}
