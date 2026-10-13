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
            Require(PlanList.Items.Count >= 4, "Real pipeline plan must be visible in the WPF inspector");
            Mark("pipeline plan explorer populated from imported manifest");

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
            Require(!checkedDetails.GetProperty("ready_build").GetBoolean() &&
                    !checkedDetails.GetProperty("ready_setup").GetBoolean() &&
                    !BuildButton.IsEnabled && !SetupButton.IsEnabled &&
                    ReviewImportButton.IsEnabled,
                "Build must stay locked after tests until external review");
            Mark("successful checks do not bypass human review");

            // CI simulates a human review only on the isolated synthetic fixture.
            // A real user obtains a review decision only after submitting the report to ChatGPT.
            var request = await EngineBridge.RunAsync("review-request", id);
            var reviewPath = Path.Combine(Path.GetTempPath(), "kentro-ci-review-"+ Guid.NewGuid().ToString("N")+".json");
            try
            {
                var ticket = JsonSerializer.Serialize(new {
                    schema = Value(request, "schema"),
                    decision = "APPROVED_FOR_BUILD",
                    project_id = Value(request, "project_id"),
                    run_id = Value(request, "run_id"),
                    source_sha256 = Value(request, "source_sha256"),
                    plan_sha256 = Value(request, "plan_sha256"),
                    results_sha256 = Value(request, "results_sha256"),
                    reviewer = "ChatGPT",
                    note = "CI simulated review of synthetic fixture only"
                });
                File.WriteAllText(reviewPath, ticket);
                Require(await ImportReviewAsync(reviewPath, false),
                    "WPF review import workflow rejected exact matching receipt");
            }
            finally { try { File.Delete(reviewPath); } catch {} }
            await RefreshDetailsAsync();
            checkedDetails = await EngineBridge.RunAsync("details", id);
            Require(checkedDetails.GetProperty("ready_build").GetBoolean() &&
                    BuildButton.IsEnabled && !ReviewImportButton.IsEnabled,
                "Build should unlock only after exact reviewer decision");
            Mark("manual review receipt accepted only for exact tested run");
            var firstHistory = await EngineBridge.RunAsync("history",id);
            Require(firstHistory.ValueKind == JsonValueKind.Array && firstHistory.GetArrayLength() >= 1 &&
                HistoryList.Items.Count >= 1, "Local history must show checked runs");
            Mark("verified persisted checks history and local diagnostic viewer");

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
            var completeHistory = await EngineBridge.RunAsync("history",id);
            Require(completeHistory.GetArrayLength() >= 3 && HistoryList.Items.Count >= 3,
                "History must show checks, build, setup as distinct persisted stages");
            Mark("full locally persisted multi-stage run history");

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
