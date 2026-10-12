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
    private async Task ExecuteAsync(string action, string description)
    {
        if (selected is null || busy) return;
        busy = true;
        work = new CancellationTokenSource();
        results.Items.Clear();
        stageText.Text = description;
        SetButtons();
        try
        {
            var id = selected;
            var response = await EngineBridge.RunAsync(action, id,
                text => DispatcherQueue.TryEnqueue(() => AddLog(text)), work.Token);
            if (response.ValueKind == JsonValueKind.Object &&
                response.TryGetProperty("blocked", out var denied) && denied.ValueKind == JsonValueKind.True)
                AddLog("Ο έλεγχος απέτυχε · Δεν εγκρίθηκε έκδοση.");
            else if (response.ValueKind == JsonValueKind.Object &&
                     response.TryGetProperty("run", out var buildRun) &&
                     buildRun.TryGetProperty("blocked", out var buildDenied) &&
                     buildDenied.ValueKind == JsonValueKind.True)
                AddLog("Το EXE δεν πέρασε όλους τους υποχρεωτικούς ελέγχους.");
            else
                AddLog("Η ενέργεια ολοκληρώθηκε. Οι λεπτομέρειες αποθηκεύτηκαν.");
            if (response.ValueKind == JsonValueKind.Object &&
                response.TryGetProperty("stable_folder", out var stable))
                AddLog("Ελεγμένη έκδοση στον φάκελο: " + stable.GetString());
        }
        catch (OperationCanceledException) { AddLog("Η εργασία ακυρώθηκε."); }
        catch (Exception ex) { AddLog("ΑΠΟΤΥΧΙΑ: " + ex.Message); }
        finally
        {
            busy = false;
            work?.Dispose();
            work = null;
            SetButtons();
            await RefreshDetailsAsync();
        }
    }
    private async Task ImportAsync()
    {
        if (busy) return;
        var picker = new FileOpenPicker();
        picker.FileTypeFilter.Add(".zip");
        InitializeWithWindow.Initialize(picker, WindowNative.GetWindowHandle(this));
        var file = await picker.PickSingleFileAsync();
        if (file is null) return;
        try
        {
            var preview = await EngineBridge.RunAsync("preview", file.Path);
            var count = Value(preview, "Files", "0");
            var question = new ContentDialog {
                Title = "Εισαγωγή έργου",
                Content = "Αρχείο: " + file.Name + "\nΠεριεχόμενα: " + count +
                          " αρχεία\n\nΝα προστεθεί στα τοπικά προγράμματα;",
                PrimaryButtonText = "Εισαγωγή", CloseButtonText = "Άκυρο",
                XamlRoot = ((FrameworkElement)Content).XamlRoot
            };
            if (await question.ShowAsync() != ContentDialogResult.Primary) return;
            busy = true;
            SetButtons();
            var result = await EngineBridge.RunAsync("import", file.Path);
            selected = Value(result, "id");
            AddLog("Εισήχθη: " + Value(result, "name", file.Name));
        }
        catch (Exception ex) { await ShowErrorAsync(ex.Message); }
        finally
        {
            busy = false;
            await RefreshProjectsAsync();
        }
    }
    private async Task OpenReportAsync()
    {
        if (selected is null || busy) return;
        try
        {
            var reply = await EngineBridge.RunAsync("report", selected);
            var path = Value(reply, "path");
            if (!File.Exists(path)) throw new FileNotFoundException("Δεν βρέθηκε η αναφορά.", path);
            Process.Start(new ProcessStartInfo { FileName = "explorer.exe", UseShellExecute = true,
                ArgumentList = { "/select,", path } });
            AddLog("Η αναφορά αποθηκεύτηκε: " + path);
        }
        catch (Exception ex) { await ShowErrorAsync(ex.Message); }
    }
    private async Task OpenFolderAsync()
    {
        if (selected is null || busy) return;
        try
        {
            var reply = await EngineBridge.RunAsync("folder", selected);
            var path = Value(reply, "path");
            if (!Directory.Exists(path)) throw new DirectoryNotFoundException(path);
            Process.Start(new ProcessStartInfo { FileName = "explorer.exe", UseShellExecute = true,
                ArgumentList = { path } });
        }
        catch (Exception ex) { await ShowErrorAsync(ex.Message); }
    }
    private async Task TrashAsync()
    {
        if (selected is null || busy) return;
        var ask = new ContentDialog {
            Title = "Αφαίρεση προγράμματος",
            Content = "Το πρόγραμμα θα μεταφερθεί στον χώρο προσωρινά αφαιρεμένων, όχι σε οριστική διαγραφή.",
            PrimaryButtonText = "Αφαίρεση", CloseButtonText = "Άκυρο",
            XamlRoot = ((FrameworkElement)Content).XamlRoot
        };
        if (await ask.ShowAsync() != ContentDialogResult.Primary) return;
        try { await EngineBridge.RunAsync("trash", selected); selected = null; await RefreshProjectsAsync(); }
        catch (Exception ex) { await ShowErrorAsync(ex.Message); }
    }
    private async Task RestoreAsync()
    {
        if (busy) return;
        try {
            var item = await EngineBridge.RunAsync("restore");
            selected = Value(item, "id");
            await RefreshProjectsAsync();
        }
        catch (Exception ex) { await ShowErrorAsync(ex.Message); }
    }
    private async Task ShowErrorAsync(string error)
    {
        var dialog = new ContentDialog { Title = "Δεν ολοκληρώθηκε η ενέργεια", Content = error,
            CloseButtonText = "Κλείσιμο", XamlRoot = ((FrameworkElement)Content).XamlRoot };
        await dialog.ShowAsync();
    }
}
