using Microsoft.UI.Xaml;

namespace KentroStudio;

public partial class App : Application
{
    private Window? _window;
    private static readonly object DiagnosticSync = new();

    internal static void RecordCrash(string stage, Exception? exception)
    {
        try
        {
            var folder = Environment.GetEnvironmentVariable("KENTRO_STUDIO_DIAG_DIR");
            if (string.IsNullOrWhiteSpace(folder))
                folder = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "KentroProgrammatismou_Clean", "diagnostics");
            Directory.CreateDirectory(folder);
            lock (DiagnosticSync)
            {
                File.AppendAllText(Path.Combine(folder, "studio-crash.log"),
                    $"[{DateTimeOffset.Now:O}] {stage}{Environment.NewLine}{exception}{Environment.NewLine}");
            }
        }
        catch { /* Reporting must never obscure the original exception. */ }
    }

    public App()
    {
        AppDomain.CurrentDomain.UnhandledException += (_, e) =>
            RecordCrash("AppDomain unhandled", e.ExceptionObject as Exception);
        TaskScheduler.UnobservedTaskException += (_, e) =>
            RecordCrash("Unobserved task", e.Exception);
        try
        {
            InitializeComponent();
            UnhandledException += (_, e) => RecordCrash("WinUI unhandled", e.Exception);
        }
        catch (Exception e)
        {
            RecordCrash("InitializeComponent", e);
            throw;
        }
    }

    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        try
        {
            _window = new StudioWindow();
            _window.Activate();
        }
        catch (Exception e)
        {
            RecordCrash("OnLaunched", e);
            throw;
        }
    }
}
