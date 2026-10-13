using System;
using System.IO;
using System.Windows;

namespace KentroWpf;

public partial class App : Application
{
    internal static void RecordError(string origin, Exception ex)
    {
        try {
            var baseDir = Environment.GetEnvironmentVariable("KENTRO_STUDIO_DIAG_DIR");
            if (string.IsNullOrWhiteSpace(baseDir))
                baseDir = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
                    "KentroProgrammatismou_Clean", "diagnostics");
            Directory.CreateDirectory(baseDir);
            File.AppendAllText(Path.Combine(baseDir, "wpf-crash.log"),
                $"[{DateTimeOffset.Now:O}] {origin}\n{ex}\n");
        } catch { /* Diagnostics must not obscure the error. */ }
    }
    public App()
    {
        AppDomain.CurrentDomain.UnhandledException += (_, e) =>
        {
            if (e.ExceptionObject is Exception ex) RecordError("Unhandled", ex);
        };
        DispatcherUnhandledException += (_, e) => RecordError("Dispatcher", e.Exception);
        TaskScheduler.UnobservedTaskException += (_, e) => RecordError("Unobserved", e.Exception);
    }
}
