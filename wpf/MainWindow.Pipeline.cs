using System;
using System.Collections.Generic;
using System.Globalization;
using System.Linq;
using System.Text;
using System.Text.Json;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;

namespace KentroWpf;

// Local GitHub-Actions-style plan and run-history viewer. Only the helper
// creates execution plans; this window displays those plans without changing them.
public partial class MainWindow
{
    private sealed record HistoryRow(string Caption, string Status, JsonElement Entry);

    private static IEnumerable<JsonElement> ArrayProperty(JsonElement item, string name)
    {
        if (item.ValueKind == JsonValueKind.Object &&
            item.TryGetProperty(name, out var values) &&
            values.ValueKind == JsonValueKind.Array)
            return values.EnumerateArray().ToArray();
        return Array.Empty<JsonElement>();
    }

    private static string ModuleOf(JsonElement item)
    {
        var module = Value(item, "module", "");
        return string.IsNullOrWhiteSpace(module) ? "ΓΕΝΙΚΑ" : module;
    }

    private static string StepName(JsonElement item)
    {
        return Value(item, "name", Value(item, "command", "Ανώνυμος έλεγχος"));
    }

    private static string StepCommand(JsonElement item)
    {
        var args = string.Join(" ", ArrayProperty(item, "args").Select(v => v.ToString()));
        return Value(item, "command") + (args.Length > 0 ? " " + args : "");
    }

    private static int IntValue(JsonElement item, string name, int defaultValue)
    {
        if (item.ValueKind == JsonValueKind.Object &&
            item.TryGetProperty(name, out var v) && v.TryGetInt32(out int n))
            return n;
        return defaultValue;
    }

    private void ShowPlan(JsonElement manifest)
    {
        PlanList.Items.Clear();
        int count = 0;
        foreach (var step in ArrayProperty(manifest, "tests"))
        {
            var seconds = IntValue(step, "timeout_seconds", 0);
            var effective = seconds == 0 ? 90 : seconds;
            PlanList.Items.Add($"ΕΛΕΓΧΟΣ · {ModuleOf(step)} · {StepName(step)}\n" +
                $"Εντολή: {StepCommand(step)}\nΌριο: {effective} δευτ.");
            count++;
        }
        foreach (var stress in ArrayProperty(manifest, "stress"))
        {
            var restarts = Value(stress, "kind") == "restart";
            int repeats = Math.Max(IntValue(stress, "repeats", 0), restarts ? 4 : 6);
            int parallel = restarts ? 1 : Math.Max(IntValue(stress, "parallel", 0), 3);
            int seconds = IntValue(stress, "timeout_seconds", 0);
            if (seconds == 0) seconds = 90;
            var description = restarts
                ? $"Επανεκκινήσεις: {repeats}"
                : $"Επαναλήψεις: {repeats} · Παράλληλες: έως {parallel}";
            PlanList.Items.Add($"STRESS · {ModuleOf(stress)} · {StepName(stress)}\n" +
                $"{description} · Όριο ανά εκτέλεση: {seconds} δευτ.\n" +
                $"Εντολή: {StepCommand(stress)}");
            count++;
        }
        if (manifest.TryGetProperty("build", out var build) && build.ValueKind == JsonValueKind.Object)
        {
            PlanList.Items.Add($"ΚΑΤΑΣΚΕΥΗ EXE · {StepName(build)}\n" +
                $"Μόνο μετά από επιτυχείς ελέγχους.\nΕντολή: {StepCommand(build)}");
            count++;
        }
        if (manifest.TryGetProperty("smoke", out var smoke) && smoke.ValueKind == JsonValueKind.Object)
        {
            PlanList.Items.Add($"ΠΡΑΓΜΑΤΙΚΗ ΕΚΚΙΝΗΣΗ · {StepName(smoke)}\n" +
                $"Εντολή: {StepCommand(smoke)}");
            count++;
        }
        if (manifest.TryGetProperty("setup", out var setup) && setup.ValueKind == JsonValueKind.Object)
        {
            PlanList.Items.Add($"ΠΡΟΑΙΡΕΤΙΚΟ SETUP · {StepName(setup)}\n" +
                $"Εντολή: {StepCommand(setup)}");
            count++;
        }
        if (count == 0)
            PlanList.Items.Add("Δεν έχει οριστεί σχέδιο ελέγχων. Η κατασκευή δεν μπορεί να εγκριθεί.");
    }

    private static string StageLabel(string stage) => stage switch
    {
        "CHECKS" => "ΕΛΕΓΧΟΙ / STRESS",
        "BUILD" => "ΚΑΤΑΣΚΕΥΗ EXE",
        "SETUP" => "SETUP",
        _ => "ΠΑΛΑΙΟ ΣΥΝΟΛΙΚΟ"
    };

    private static string CategoryLabel(string category) => category switch
    {
        "TIMEOUT" => "ΧΡΟΝΙΚΟ ΟΡΙΟ",
        "CANCELLED" => "ΑΚΥΡΩΣΗ",
        "INTEGRITY" => "ΑΚΕΡΑΙΟΤΗΤΑ",
        "NOT_RUN" => "ΔΕΝ ΕΚΤΕΛΕΣΤΗΚΕ",
        _ => "ΑΠΟΤΥΧΙΑ ΔΟΚΙΜΗΣ"
    };

    private async Task RefreshHistoryAsync(string projectId)
    {
        JsonElement data;
        try
        {
            data = await EngineBridge.RunAsync("history", projectId);
        }
        catch (Exception ex)
        {
            if (_integration) throw;
            HistoryStatus.Text = "Δεν ήταν δυνατή η ανάγνωση ιστορικού: " + ex.Message;
            return;
        }
        if (_selected != projectId || data.ValueKind != JsonValueKind.Array) return;
        var rows = new List<HistoryRow>();
        foreach (var entry in data.EnumerateArray())
        {
            var run = entry.GetProperty("run");
            var timestamp = Value(run, "started");
            var when = timestamp;
            if (DateTimeOffset.TryParse(timestamp, CultureInfo.InvariantCulture,
                    DateTimeStyles.None, out var parsed))
                when = parsed.ToLocalTime().ToString("dd/MM/yyyy HH:mm");
            var results = ArrayProperty(run, "results").ToArray();
            int passed = results.Count(item => Value(item, "status") == "PASS");
            var summary = Value(entry, "summary", "Άγνωστο αποτέλεσμα");
            var issues = ArrayProperty(entry, "issues").ToArray();
            var type = issues.Length > 0
                ? CategoryLabel(Value(issues[0], "category"))
                : "ΧΩΡΙΣ ΑΠΟΤΥΧΙΕΣ";
            rows.Add(new HistoryRow($"{when} · {StageLabel(Value(run, "stage"))}",
                $"{summary} · {passed}/{results.Length} PASS · {type}", entry.Clone()));
        }
        HistoryList.ItemsSource = rows;
        HistoryStatus.Text = rows.Count == 0
            ? "Δεν υπάρχουν ακόμη εκτελέσεις για το επιλεγμένο έργο."
            : $"Οι τελευταίες {rows.Count} εκτελέσεις · επιλογή για λεπτομέρειες";
        if (rows.Count > 0) HistoryList.SelectedIndex = 0;
        else HistoryDetail.Text = "Δεν υπάρχουν αποτελέσματα.";
    }

    private void History_SelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        if (HistoryList.SelectedItem is not HistoryRow selected) return;
        var run = selected.Entry.GetProperty("run");
        var lines = new StringBuilder();
        lines.AppendLine(selected.Caption);
        lines.AppendLine(selected.Status);
        lines.AppendLine();
        var issues = ArrayProperty(selected.Entry, "issues").ToArray();
        if (issues.Length == 0) lines.AppendLine("Δεν καταγράφηκαν αποτυχίες σε αυτή την εκτέλεση.");
        foreach (var issue in issues)
        {
            lines.AppendLine($"{CategoryLabel(Value(issue, "category"))} · " +
                $"{Value(issue, "phase")} · {Value(issue, "name")}");
            lines.AppendLine(Value(issue, "explanation"));
            var detail = Value(issue, "detail");
            if (!string.IsNullOrWhiteSpace(detail)) lines.AppendLine("Στοιχείο: " + detail);
            var logfile = Value(issue, "log_file");
            if (!string.IsNullOrWhiteSpace(logfile))
                lines.AppendLine("Αρχείο: logs/" + logfile + " (στην αναφορά)");
            lines.AppendLine();
        }
        lines.AppendLine("Αρχεία δοκιμών και αναλυτικά logs: πάτησε ΑΝΑΦΟΡΑ.");
        HistoryDetail.Text = lines.ToString();
    }
}
