using System;
using System.Diagnostics;
using System.IO;
using System.Linq;
using System.Text.Json;
using System.Threading;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using Microsoft.Win32;

namespace KentroWpf;

public partial class MainWindow : Window
{
    private sealed record ProjectRow(string Id, string Name, string Info);
    private string? _selected;
    private bool _busy;
    private bool _integration;
    private CancellationTokenSource? _work;
    private bool _refreshing;

    public MainWindow()
    {
        InitializeComponent();
        var bounds = SystemParameters.WorkArea;
        Width = Math.Min(1180, Math.Max(780, bounds.Width - 55));
        Height = Math.Min(780, Math.Max(540, bounds.Height - 55));
        Left = bounds.Left + (bounds.Width - Width) / 2;
        Top = bounds.Top + (bounds.Height - Height) / 2;
        SizeChanged += (_, _) => Phases.Columns = ActualWidth < 1100 ? 2 : 4;
        Loaded += (_, _) =>
        {
            var zip = Environment.GetEnvironmentVariable("KENTRO_WPF_INTEGRATION_ZIP");
            if (!string.IsNullOrWhiteSpace(zip))
            {
                _integration = true;
                _ = RunCiIntegrationAsync(zip);
            }
            else _ = RefreshProjectsAsync();
        };
        SetButtons();
    }

    private static string Value(JsonElement element, string property, string fallback = "")
    {
        if(element.ValueKind == JsonValueKind.Object &&
           element.TryGetProperty(property, out var v) &&
           v.ValueKind is not (JsonValueKind.Null or JsonValueKind.Undefined))
            return v.ToString();
        return fallback;
    }

    private void AddLog(string line)
    {
        ResultsList.Items.Add(line);
        if (ResultsList.Items.Count > 400) ResultsList.Items.RemoveAt(0);
        if (ResultsList.Items.Count > 0)
            ResultsList.ScrollIntoView(ResultsList.Items[^1]);
    }

    private void SetButtons()
    {
        bool selected = !_busy && !string.IsNullOrEmpty(_selected);
        ImportButton.IsEnabled = !_busy;
        ReloadButton.IsEnabled = !_busy;
        RestoreButton.IsEnabled = !_busy;
        TrashButton.IsEnabled = selected;
        TestButton.IsEnabled = selected;
        ReportButton.IsEnabled = false;
        BuildButton.IsEnabled = false;
        SetupButton.IsEnabled = false;
        FolderButton.IsEnabled = selected;
        StopButton.IsEnabled = _busy;
        Progress.Visibility = _busy ? Visibility.Visible : Visibility.Collapsed;
    }

    private async Task RefreshProjectsAsync()
    {
        if (_busy || _refreshing) return;
        _refreshing = true;
        try
        {
            var data = await EngineBridge.RunAsync("list");
            if (data.ValueKind != JsonValueKind.Array)
                throw new InvalidDataException("Η μηχανή δεν επέστρεψε λίστα προγραμμάτων.");
            var previous = _selected;
            var items = data.EnumerateArray()
                .Select(p => new ProjectRow(Value(p,"id"),Value(p,"name","Πρόγραμμα"),
                    Value(p,"version","χωρίς έκδοση")+" · "+Value(p,"language","Άγνωστο")))
                .ToArray();
            ProjectsList.ItemsSource = items;
            CounterText.Text = items.Length == 1 ? "1 ΕΡΓΟ" : items.Length + " ΕΡΓΑ";
            var selected = items.FirstOrDefault(p => p.Id == previous) ?? items.FirstOrDefault();
            _selected = selected?.Id;
            ProjectsList.SelectedItem = selected;
            if (selected is null)
            {
                ProjectNameText.Text = "Δεν έχει εισαχθεί πρόγραμμα";
                ProjectInfoText.Text = "Πατήστε «Εισαγωγή προγράμματος ZIP»";
                StageText.Text = "Επόμενο βήμα: εισαγωγή ZIP έργου";
                SetButtons();
            }
            else await RefreshDetailsAsync();
        }
        catch (Exception ex)
        {
            App.RecordError("RefreshProjects",ex);
            if (_integration) throw;
            AddLog("ΑΠΟΤΥΧΙΑ ΑΝΑΝΕΩΣΗΣ: " + ex.Message);
        }
        finally { _refreshing = false; }
    }

    private async Task RefreshDetailsAsync()
    {
        SetButtons();
        if (string.IsNullOrWhiteSpace(_selected) || _busy) return;
        var id = _selected;
        try
        {
            var details = await EngineBridge.RunAsync("details",id);
            if (_busy || _selected != id) return;
            var project = details.GetProperty("project");
            var manifest = details.GetProperty("manifest");
            ProjectNameText.Text = Value(project,"name","Πρόγραμμα");
            int tests = manifest.TryGetProperty("tests",out var a) && a.ValueKind == JsonValueKind.Array ? a.GetArrayLength() : 0;
            int stress = manifest.TryGetProperty("stress",out var b) && b.ValueKind == JsonValueKind.Array ? b.GetArrayLength() : 0;
            ProjectInfoText.Text = "Έκδοση "+Value(project,"version","—")+" · "+
                Value(project,"language","Άγνωστο")+" · "+
                tests+" λειτουργικοί έλεγχοι · "+stress+" δοκιμές πίεσης";
            var checks = details.GetProperty("ready_build").GetBoolean();
            var setup = details.GetProperty("ready_setup").GetBoolean();
            TestButton.IsEnabled = true;
            BuildButton.IsEnabled = checks;
            SetupButton.IsEnabled = setup;
            FolderButton.IsEnabled = true;
            TrashButton.IsEnabled = true;
            var latest = details.GetProperty("latest");
            ReportButton.IsEnabled = latest.ValueKind == JsonValueKind.Object && Value(latest,"id") != "";
            StageText.Text = setup ? "Επιτυχής κατασκευή EXE · Διαθέσιμο Setup" :
                checks ? "Οι έλεγχοι πέρασαν · Μπορεί να κατασκευαστεί EXE" :
                "Επόμενο βήμα: λειτουργικοί έλεγχοι και stress tests";
        }
        catch(Exception ex)
        {
            App.RecordError("RefreshDetails",ex);
            if (_integration) throw;
            AddLog("ΑΠΟΤΥΧΙΑ ΑΝΑΓΝΩΣΗΣ: " + ex.Message);
        }
    }

    private async void Projects_SelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        if (_refreshing || _busy) return;
        _selected = (ProjectsList.SelectedItem as ProjectRow)?.Id;
        await RefreshDetailsAsync();
    }

    private async Task ExecuteAsync(string action, string title)
    {
        if (_busy || string.IsNullOrEmpty(_selected)) return;
        var id = _selected;
        _busy = true;
        _work = new CancellationTokenSource();
        ResultsList.Items.Clear();
        StageText.Text = title;
        SetButtons();
        try
        {
            var response = await EngineBridge.RunAsync(action,id,
                message => Dispatcher.BeginInvoke(new Action(() => AddLog(message))),_work.Token);
            bool blocked = response.ValueKind == JsonValueKind.Object &&
                response.TryGetProperty("blocked",out var p) && p.ValueKind == JsonValueKind.True;
            if (response.ValueKind == JsonValueKind.Object &&
                response.TryGetProperty("run",out var run) &&
                run.TryGetProperty("blocked",out var denied) && denied.ValueKind == JsonValueKind.True)
                blocked = true;
            AddLog(blocked ? "Οι υποχρεωτικοί έλεγχοι απέτυχαν · Δεν εγκρίθηκε έκδοση." :
                "Η ενέργεια ολοκληρώθηκε. Τα αποτελέσματα αποθηκεύτηκαν.");
            if (response.ValueKind == JsonValueKind.Object &&
                response.TryGetProperty("stable_folder",out var folder))
                AddLog("Ελεγμένη έκδοση στον φάκελο: "+folder.GetString());
        }
        catch (OperationCanceledException) { AddLog("Η εργασία ακυρώθηκε."); if (_integration) throw; }
        catch (Exception ex)
        {
            App.RecordError("Execute "+action,ex);
            AddLog("ΑΠΟΤΥΧΙΑ: "+ex.Message);
            if (_integration) throw;
        }
        finally
        {
            _busy = false;
            _work?.Dispose();
            _work = null;
            SetButtons();
            await RefreshDetailsAsync();
        }
    }

    private async Task ImportAsync(string path, bool confirmation)
    {
        if (_busy) return;
        try
        {
            var preview = await EngineBridge.RunAsync("preview",path);
            if (confirmation && MessageBox.Show(this,
                "Αρχείο: "+Path.GetFileName(path)+"\nΠεριεχόμενα: "+Value(preview,"Files","0")+
                " αρχεία\n\nΝα προστεθεί στα τοπικά προγράμματα;",
                "Εισαγωγή έργου",MessageBoxButton.YesNo,MessageBoxImage.Question)
                != MessageBoxResult.Yes) return;
            _busy = true;
            SetButtons();
            var imported = await EngineBridge.RunAsync("import",path);
            _selected = Value(imported,"id");
            AddLog("Εισήχθη: "+Value(imported,"name",Path.GetFileName(path)));
        }
        catch(Exception ex)
        {
            App.RecordError("Import",ex);
            if (_integration) throw;
            MessageBox.Show(this,ex.Message,"Αποτυχία εισαγωγής",MessageBoxButton.OK,MessageBoxImage.Error);
        }
        finally
        {
            _busy = false;
            SetButtons();
            await RefreshProjectsAsync();
            // The results list can request BringIntoView during import. On a new project,
            // return the main panel to its start so the selected project card is visible.
            MainScroll.ScrollToTop();
        }
    }

    private async Task<string> ExportReportAsync(bool reveal)
    {
        if (_busy || _selected is null) return "";
        try
        {
            var result = await EngineBridge.RunAsync("report",_selected);
            var path = Value(result,"path");
            if(!File.Exists(path)) throw new FileNotFoundException("Η αναφορά δεν βρέθηκε.",path);
            AddLog("Η αναφορά αποθηκεύτηκε: "+path);
            if (reveal)
            {
                var p = new ProcessStartInfo("explorer.exe") { UseShellExecute=true };
                p.ArgumentList.Add("/select,");
                p.ArgumentList.Add(path);
                Process.Start(p);
            }
            return path;
        }
        catch(Exception ex)
        {
            App.RecordError("Report",ex);
            if (_integration) throw;
            MessageBox.Show(this,ex.Message,"Δεν βρέθηκε αναφορά",MessageBoxButton.OK,MessageBoxImage.Error);
            return "";
        }
    }

    private async Task TrashAsync(bool confirmation)
    {
        if(_busy || _selected is null) return;
        if (confirmation && MessageBox.Show(this,
            "Το πρόγραμμα θα μεταφερθεί στα προσωρινά αφαιρεμένα, χωρίς οριστική διαγραφή.",
            "Αφαίρεση προγράμματος",MessageBoxButton.YesNo,MessageBoxImage.Warning)
            != MessageBoxResult.Yes) return;
        try
        {
            await EngineBridge.RunAsync("trash",_selected);
            _selected = null;
            await RefreshProjectsAsync();
        }
        catch(Exception ex)
        {
            App.RecordError("Trash",ex);
            if (_integration) throw;
            MessageBox.Show(this,ex.Message,"Αποτυχία αφαίρεσης",MessageBoxButton.OK,MessageBoxImage.Error);
        }
    }

    private async Task RestoreAsync()
    {
        if (_busy) return;
        try
        {
            var p = await EngineBridge.RunAsync("restore");
            _selected = Value(p,"id");
            await RefreshProjectsAsync();
        }
        catch(Exception ex)
        {
            App.RecordError("Restore",ex);
            if (_integration) throw;
            MessageBox.Show(this,ex.Message,"Αποτυχία επαναφοράς",MessageBoxButton.OK,MessageBoxImage.Error);
        }
    }

    private async void Import_Click(object sender, RoutedEventArgs e)
    {
        var dialog = new OpenFileDialog { Title="Εισαγωγή πηγαίου κώδικα προγράμματος",
            Filter="Αρχεία ZIP (*.zip)|*.zip",CheckFileExists=true };
        if (dialog.ShowDialog(this) == true) await ImportAsync(dialog.FileName,true);
    }
    private async void Reload_Click(object sender,RoutedEventArgs e) => await RefreshProjectsAsync();
    private async void Restore_Click(object sender,RoutedEventArgs e) => await RestoreAsync();
    private async void Trash_Click(object sender,RoutedEventArgs e) => await TrashAsync(true);
    private async void Checks_Click(object sender,RoutedEventArgs e) => await ExecuteAsync("checks","Έλεγχοι και stress tests");
    private async void Build_Click(object sender,RoutedEventArgs e) => await ExecuteAsync("build","Κατασκευή και δοκιμή πραγματικού EXE");
    private async void Setup_Click(object sender,RoutedEventArgs e) => await ExecuteAsync("setup","Κατασκευή προαιρετικού Setup");
    private async void Report_Click(object sender,RoutedEventArgs e) => await ExportReportAsync(true);
    private async void Folder_Click(object sender,RoutedEventArgs e)
    {
        if(_selected is null || _busy) return;
        try
        {
            var result = await EngineBridge.RunAsync("folder",_selected);
            var path = Value(result,"path");
            if(!Directory.Exists(path)) throw new DirectoryNotFoundException(path);
            Process.Start(new ProcessStartInfo("explorer.exe") { UseShellExecute=true,
                ArgumentList = { path } });
        }
        catch(Exception ex)
        {
            App.RecordError("Folder",ex);
            MessageBox.Show(this,ex.Message,"Δεν βρέθηκε φάκελος",MessageBoxButton.OK,MessageBoxImage.Error);
        }
    }
    private void Stop_Click(object sender,RoutedEventArgs e) => _work?.Cancel();
}
