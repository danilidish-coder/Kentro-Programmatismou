using System;
using System.Collections.Generic;
using System.IO;
using System.Text.Json;
using System.Threading.Tasks;

namespace KentroStudio;

// Runner-only in-process integration scenario. This exercises the real
// window state, buttons/gates, presentation, IPC bridge and Go engine together.
// In ordinary use the environment variable is absent: this code never runs.
internal sealed partial class StudioWindow
{
    private async Task RunCiIntegrationAsync(string archivePath)
    {
        var output = Environment.GetEnvironmentVariable("KENTRO_STUDIO_INTEGRATION_RESULT");
        var lines = new List<string>();
        var code = 1;
        static void Require(bool condition, string meaning)
        {
            if (!condition) throw new InvalidOperationException("UI INTEGRATION FAILURE: " + meaning);
        }
        void Mark(string stage) => lines.Add("PASS: " + stage);
        try
        {
            await Task.Delay(1500);
            Require(File.Exists(archivePath), "Missing test ZIP fixture");
            var before = await EngineBridge.RunAsync("list");
            Require(before.ValueKind == JsonValueKind.Array && before.GetArrayLength() == 0,
                "The UI test must start with isolated empty project data");
            Mark("isolated local state");

            var preview = await EngineBridge.RunAsync("preview", archivePath);
            Require(preview.TryGetProperty("Files", out var fileCount) && fileCount.GetInt32() >= 5,
                "ZIP preview through UI bridge");
            var imported = await EngineBridge.RunAsync("import", archivePath);
            var id = Value(imported, "id");
            Require(id.Length >= 8, "ZIP import project identifier");
            await RefreshProjectsAsync();
            selected = id;
            await RefreshDetailsAsync();
            Require(projects.Items.Count == 1 &&
                    projectName.Text == Value(imported, "name") &&
                    testButton.IsEnabled &&
                    !buildButton.IsEnabled &&
                    !setupButton.IsEnabled,
                "Imported project displayed with correct locked actions");
            Mark("project list, imported identity and initial UI gates");

            await ExecuteAsync("checks", "Δοκιμή ελέγχων εφαρμογής");
            var afterChecks = await EngineBridge.RunAsync("details", id);
            Require(afterChecks.GetProperty("ready_build").GetBoolean() &&
                    !afterChecks.GetProperty("ready_setup").GetBoolean() &&
                    buildButton.IsEnabled && !setupButton.IsEnabled,
                "The UI should unlock EXE only after checks and stress pass");
            Mark("real test/stress execution through native UI and build gate");

            await ExecuteAsync("build", "Δοκιμή δημιουργίας EXE");
            var afterBuild = await EngineBridge.RunAsync("details", id);
            Require(afterBuild.GetProperty("ready_setup").GetBoolean() && setupButton.IsEnabled,
                "Validated EXE should unlock Setup in the UI");
            Mark("UI-driven build, PE validation, actual smoke, release readiness");

            await ExecuteAsync("setup", "Δοκιμή δημιουργίας Setup");
            var afterSetup = await EngineBridge.RunAsync("details", id);
            var latest = afterSetup.GetProperty("latest");
            Require(Value(latest, "stage") == "SETUP" && !latest.GetProperty("blocked").GetBoolean(),
                "Independent setup stage must pass via UI");
            Mark("UI-driven optional Setup stage");

            var report = await EngineBridge.RunAsync("report", id);
            var reportPath = Value(report, "path");
            lines.Add("DEBUG: report payload=" + report.GetRawText());
            lines.Add("DEBUG: resolved path=" + reportPath);
            lines.Add("DEBUG: report-path length=" + reportPath.Length);
            var reportDirectory = Path.GetDirectoryName(reportPath) ?? "";
            lines.Add("DEBUG: parent-exists=" + Directory.Exists(reportDirectory));
            lines.Add("DEBUG: expected-filename-utf8=" + Convert.ToHexString(System.Text.Encoding.UTF8.GetBytes(Path.GetFileName(reportPath))));
            if (Directory.Exists(reportDirectory))
            {
                foreach (var actualFile in Directory.EnumerateFiles(reportDirectory))
                {
                    lines.Add("DEBUG: disk-filename=" + Path.GetFileName(actualFile));
                    lines.Add("DEBUG: disk-filename-utf8=" + Convert.ToHexString(System.Text.Encoding.UTF8.GetBytes(Path.GetFileName(actualFile))));
                }
            }
            lines.Add("DEBUG: localappdata=" + Environment.GetEnvironmentVariable("LOCALAPPDATA"));
            Require(File.Exists(reportPath), "Exported report must exist: " + reportPath);
            var backup = await EngineBridge.RunAsync("backup", id);
            Require(File.Exists(Path.Combine(Value(backup, "path"), "backup.json")),
                "Backup must preserve integrity manifest");
            Mark("report and verified backup");

            await EngineBridge.RunAsync("trash", id);
            selected = null;
            await RefreshProjectsAsync();
            Require(projects.Items.Count == 0 && !testButton.IsEnabled,
                "Removed project should disappear and disable UI actions");
            var restored = await EngineBridge.RunAsync("restore");
            Require(Value(restored, "id") == id, "Project restoration keeps identity");
            await RefreshProjectsAsync();
            selected = id;
            await RefreshDetailsAsync();
            Require(projects.Items.Count == 1 && testButton.IsEnabled,
                "Restored project must reappear with functioning UI gates");
            Mark("removal, restoration, list refresh and state synchronization");
            lines.Add("PASS: ALL NATIVE GUI + ENGINE MODULES INTEGRATED");
            code = 0;
        }
        catch (Exception e)
        {
            lines.Add("FAIL: " + e);
            App.RecordCrash("CI UI integration", e);
        }
        finally
        {
            if (!string.IsNullOrWhiteSpace(output))
            {
                try { File.WriteAllLines(output, lines); }
                catch (Exception e) { App.RecordCrash("Saving CI UI report", e); code = 1; }
            }
            Environment.Exit(code);
        }
    }
}
