using Microsoft.UI;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Media.Animation;
using Windows.Storage.Pickers;
using WinRT.Interop;
using System.Diagnostics;
using System.Numerics;
using System.Text.Json;

namespace KentroStudio;

internal sealed partial class StudioWindow
{
    private static string Value(JsonElement element, string property, string fallback = "")
    {
        if (element.ValueKind == JsonValueKind.Object &&
            element.TryGetProperty(property, out var item) &&
            item.ValueKind is not (JsonValueKind.Null or JsonValueKind.Undefined))
            return item.ToString();
        return fallback;
    }
    private void AddLog(string entry)
    {
        var value = new TextBlock {
            Text = entry, FontFamily = new FontFamily("Segoe UI"), FontSize = 13,
            Foreground = Ink, TextWrapping = TextWrapping.Wrap,
            Margin = new Thickness(10, 7, 10, 7)
        };
        results.Items.Add(value);
        if (results.Items.Count > 400) results.Items.RemoveAt(0);
        if (results.Items.Count > 0) results.ScrollIntoView(results.Items[^1]);
    }
    private async Task RefreshProjectsAsync()
    {
        if (busy) return;
        try
        {
            var data = await EngineBridge.RunAsync("list");
            var previous = selected;
            projects.Items.Clear();
            ListViewItem? selectedItem = null;
            var total = 0;
            foreach (var element in data.EnumerateArray())
            {
                total++;
                var name = Value(element, "name", "Πρόγραμμα");
                var info = Value(element, "version", "χωρίς έκδοση") + " · " + Value(element, "language", "Άγνωστο");
                var row = new StackPanel { Spacing = 5, Margin = new Thickness(6, 8, 6, 8) };
                row.Children.Add(Label(name, 15, true));
                row.Children.Add(Label(info, 12));
                var item = new ListViewItem {
                    Tag = Value(element, "id"), Content = row,
                    HorizontalContentAlignment = HorizontalAlignment.Stretch,
                    Margin = new Thickness(3, 3, 3, 3)
                };
                projects.Items.Add(item);
                if (item.Tag is string id && id == previous) selectedItem = item;
                if (total == 1 && selectedItem is null) selectedItem = item;
            }
            counter.Text = total == 1 ? "1 ΕΡΓΟ" : total + " ΕΡΓΑ";
            selected = null;
            if (selectedItem is not null) projects.SelectedItem = selectedItem;
            else
            {
                projectName.Text = "Δεν έχει εισαχθεί πρόγραμμα";
                projectInfo.Text = "Πατήστε «Εισαγωγή προγράμματος ZIP»";
                stageText.Text = "Αναμονή εισαγωγής";
                SetButtons();
            }
        }
        catch (Exception ex) { await ShowErrorAsync(ex.Message); }
    }
    private async Task RefreshDetailsAsync()
    {
        SetButtons();
        if (selected is null || busy) return;
        try
        {
            var details = await EngineBridge.RunAsync("details", selected);
            var project = details.GetProperty("project");
            var plan = details.GetProperty("manifest");
            projectName.Text = Value(project, "name", "Πρόγραμμα");
            var tests = plan.TryGetProperty("tests", out var t) && t.ValueKind == JsonValueKind.Array ? t.GetArrayLength() : 0;
            var stress = plan.TryGetProperty("stress", out var s) && s.ValueKind == JsonValueKind.Array ? s.GetArrayLength() : 0;
            projectInfo.Text = "Έκδοση " + Value(project, "version", "—") + "  ·  " +
                Value(project, "language", "Άγνωστο") + "  ·  " +
                tests + " λειτουργικοί έλεγχοι  ·  " + stress + " δοκιμές πίεσης";
            var checksOK = details.GetProperty("ready_build").GetBoolean();
            var setupOK = details.GetProperty("ready_setup").GetBoolean();
            testButton.IsEnabled = true;
            reportButton.IsEnabled = details.GetProperty("latest").ValueKind == JsonValueKind.Object &&
                Value(details.GetProperty("latest"), "id") != "";
            buildButton.IsEnabled = checksOK;
            setupButton.IsEnabled = setupOK;
            folderButton.IsEnabled = true;
            trashButton.IsEnabled = true;
            stageText.Text = setupOK ? "Επιτυχής κατασκευή EXE · Διαθέσιμο Setup" :
                checksOK ? "Οι έλεγχοι ολοκληρώθηκαν · Μπορεί να κατασκευαστεί EXE" :
                "Επόμενο βήμα: λειτουργικοί έλεγχοι και stress tests";
        }
        catch (Exception ex) { AddLog("Σφάλμα ανάγνωσης έργου: " + ex.Message); }
    }
    private void SetButtons()
    {
        var available = !busy && selected is not null;
        testButton.IsEnabled = available;
        buildButton.IsEnabled = false;
        setupButton.IsEnabled = false;
        reportButton.IsEnabled = false;
        folderButton.IsEnabled = available;
        trashButton.IsEnabled = available;
        stopButton.IsEnabled = busy;
        progress.Visibility = busy ? Visibility.Visible : Visibility.Collapsed;
    }
}
