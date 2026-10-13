using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Text.Json;
using System.Threading.Tasks;
using System.Windows.Media;
using System.Windows.Media.Imaging;

namespace KentroWpf;

public partial class MainWindow
{
    // Only active on a Windows CI runner, never in normal use.
    private async Task RunCiIntegrationAsync(string fixture)
    {
        var logPath = Environment.GetEnvironmentVariable("KENTRO_WPF_INTEGRATION_RESULT");
        var screenshot = Environment.GetEnvironmentVariable("KENTRO_WPF_SCREENSHOT");
        var lines = new List<string>();
        int exitCode = 1;
        static void Require(bool condition,string text)
        {
            if (!condition) throw new InvalidOperationException("WPF INTEGRATION FAILURE: "+text);
        }
        void Mark(string phase) => lines.Add("PASS: "+phase);
        try
        {
            await Task.Delay(1100);
            Require(File.Exists(fixture),"Missing ZIP fixture");
            var empty = await EngineBridge.RunAsync("list");
            Require(empty.ValueKind == JsonValueKind.Array && empty.GetArrayLength() == 0,
                "Isolated WPF workspace must initially be empty");
            Mark("isolated empty project workspace");

            await ImportAsync(fixture,false);
            Require(!string.IsNullOrWhiteSpace(_selected) && ProjectsList.Items.Count == 1 &&
                TestButton.IsEnabled && !BuildButton.IsEnabled && !SetupButton.IsEnabled,
                "WPF import, project list, and gated buttons");
            var id = _selected!;
            var imported = await EngineBridge.RunAsync("details",id);
            Require(ProjectNameText.Text == Value(imported.GetProperty("project"),"name"),
                "WPF project name and native controls");
            Mark("actual UI import and initial safety gates");

            if (!string.IsNullOrWhiteSpace(screenshot))
            {
                Directory.CreateDirectory(Path.GetDirectoryName(screenshot)!);
                UpdateLayout();
                var bitmap = new RenderTargetBitmap(Math.Max(1,(int)Math.Ceiling(ActualWidth)),
                    Math.Max(1,(int)Math.Ceiling(ActualHeight)),96,96,PixelFormats.Pbgra32);
                bitmap.Render(this);
                var png = new PngBitmapEncoder();
                png.Frames.Add(BitmapFrame.Create(bitmap));
                using var stream = File.Create(screenshot);
                png.Save(stream);
                Mark("real WPF window rendered to PNG");
            }

            await ExecuteAsync("checks","CI checks and stress");
            var checkedDetails = await EngineBridge.RunAsync("details",id);
            Require(checkedDetails.GetProperty("ready_build").GetBoolean() &&
                    !checkedDetails.GetProperty("ready_setup").GetBoolean() &&
                    BuildButton.IsEnabled && !SetupButton.IsEnabled,
                "Build must unlock only after passing test/stress");
            Mark("UI-driven tests and stress unlock EXE");

            await ExecuteAsync("build","CI build and Windows PE smoke");
            var buildDetails = await EngineBridge.RunAsync("details",id);
            Require(buildDetails.GetProperty("ready_setup").GetBoolean() && SetupButton.IsEnabled,
                "Approved EXE unlocks optional Setup");
            Mark("UI-driven PE build, smoke, and release gate");

            await ExecuteAsync("setup","CI Setup");
            var setupDetails = await EngineBridge.RunAsync("details",id);
            var latest = setupDetails.GetProperty("latest");
            Require(Value(latest,"stage") == "SETUP" && !latest.GetProperty("blocked").GetBoolean(),
                "Independent Setup operation");
            Mark("UI-driven optional Setup");

            var report = await ExportReportAsync(false);
            Require(File.Exists(report),"Persistent report with Greek filename");
            var backup = await EngineBridge.RunAsync("backup",id);
            Require(File.Exists(Path.Combine(Value(backup,"path"),"backup.json")),
                "Integrity-protected backup manifest");
            Mark("UI report and real integrity backup");

            await TrashAsync(false);
            Require(ProjectsList.Items.Count == 0 && !TestButton.IsEnabled,
                "Soft-delete removes project from WPF list");
            await RestoreAsync();
            Require(_selected == id && ProjectsList.Items.Count == 1 && TestButton.IsEnabled,
                "WPF soft-deletion and restore preserve identity");
            Mark("UI-driven reversible removal and restore");

            lines.Add("PASS: ALL WPF GUI + GO ENGINE MODULES INTEGRATED");
            exitCode = 0;
        }
        catch(Exception ex)
        {
            lines.Add("FAIL: "+ex);
            App.RecordError("WPF CI end-to-end",ex);
        }
        finally
        {
            if (!string.IsNullOrWhiteSpace(logPath))
            {
                try { File.WriteAllLines(logPath,lines); }
                catch(Exception ex) { App.RecordError("Writing WPF CI log",ex); exitCode=1; }
            }
            Environment.Exit(exitCode);
        }
    }
}
