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

// UI-only layer: no duplicated build, approval, or ZIP logic.
internal sealed class StudioWindow : Window
{
    private readonly ListView projects = new();
    private readonly ListView results = new();
    private readonly TextBlock projectName = Label("Δεν έχει επιλεγεί πρόγραμμα", 25, true);
    private readonly TextBlock projectInfo = Label("Εισάγετε ZIP για να ξεκινήσετε.", 14);
    private readonly TextBlock stageText = Label("Αναμονή εισαγωγής έργου", 14, true);
    private readonly TextBlock logHeading = Label("Αποτελέσματα εργασίας", 18, true);
    private readonly ProgressBar progress = new() { IsIndeterminate = true, Visibility = Visibility.Collapsed, Height = 4 };
    private readonly Button testButton = ActionButton("ΕΚΤΕΛΕΣΗ ΕΛΕΓΧΩΝ", true);
    private readonly Button buildButton = ActionButton("ΚΑΤΑΣΚΕΥΗ EXE", true);
    private readonly Button setupButton = ActionButton("ΔΗΜΙΟΥΡΓΙΑ SETUP", false);
    private readonly Button reportButton = ActionButton("ΑΝΟΙΓΜΑ ΑΝΑΦΟΡΑΣ", false);
    private readonly Button folderButton = ActionButton("ΦΑΚΕΛΟΣ ΠΡΟΓΡΑΜΜΑΤΟΣ", false);
    private readonly Button trashButton = ActionButton("ΑΦΑΙΡΕΣΗ", false);
    private readonly Button stopButton = ActionButton("ΑΚΥΡΩΣΗ", false);
    private readonly TextBlock counter = Label("0 ΕΡΓΑ", 13, true);
    private readonly TextBlock status = Label("Τοπική εργασία · Χωρίς εξάρτηση από GitHub", 12);
    private CancellationTokenSource? work;
    private string? selected;
    private bool busy;

    private static SolidColorBrush Color(byte r, byte g, byte b) =>
        new(Windows.UI.Color.FromArgb(255, r, g, b));
    private static readonly SolidColorBrush Ink = Color(25, 42, 69);
    private static readonly SolidColorBrush Muted = Color(105, 123, 151);
    private static readonly SolidColorBrush Blue = Color(39, 98, 219);
    private static readonly SolidColorBrush Background = Color(240, 245, 253);
    private static readonly SolidColorBrush PaleBlue = Color(230, 239, 255);
    private static readonly SolidColorBrush White = Color(255, 255, 255);

    private static TextBlock Label(string value, double size = 15, bool strong = false)
    {
        return new TextBlock {
            Text = value, FontFamily = new FontFamily("Segoe UI Variable"),
            FontSize = size, FontWeight = strong
                ? Microsoft.UI.Text.FontWeights.SemiBold : Microsoft.UI.Text.FontWeights.Normal,
            Foreground = strong ? Ink : Muted, TextWrapping = TextWrapping.Wrap
        };
    }
    private static Button ActionButton(string value, bool accent)
    {
        var b = new Button {
            Content = value, HorizontalAlignment = HorizontalAlignment.Stretch,
            Height = 47, FontFamily = new FontFamily("Segoe UI Variable"),
            FontSize = 12.5, FontWeight = Microsoft.UI.Text.FontWeights.SemiBold,
            Background = accent ? Blue : PaleBlue,
            Foreground = accent ? White : Ink,
            BorderBrush = accent ? Blue : Color(198, 214, 238),
            BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(13),
            Padding = new Thickness(13, 6, 13, 6)
        };
        b.Shadow = new ThemeShadow();
        b.Translation = new Vector3(0, 0, 12);
        b.PointerEntered += (_, _) => { if (b.IsEnabled) b.Scale = new Vector3(1.015f, 1.015f, 1); };
        b.PointerExited += (_, _) => b.Scale = Vector3.One;
        return b;
    }
    private static Border Card(UIElement body, Thickness? pad = null)
    {
        return new Border {
            Background = White, CornerRadius = new CornerRadius(20),
            BorderBrush = Color(222, 231, 244), BorderThickness = new Thickness(1),
            Padding = pad ?? new Thickness(20), Child = body,
            Shadow = new ThemeShadow(), Translation = new Vector3(0, 0, 18)
        };
    }
    private static Grid Columns(params double[] widths)
    {
        var g = new Grid();
        foreach (var width in widths) g.ColumnDefinitions.Add(new ColumnDefinition {
            Width = width == 0 ? new GridLength(1, GridUnitType.Star) : new GridLength(width)
        });
        return g;
    }

    public StudioWindow()
    {
        Title = "Κέντρο Προγραμματισμού · Studio";
        SystemBackdrop = new MicaBackdrop();
        AppWindow.Resize(new Windows.Graphics.SizeInt32(1210, 810));
        Content = BuildLayout();

        projects.SelectionMode = ListViewSelectionMode.Single;
        projects.SelectionChanged += async (_, _) =>
        {
            if (projects.SelectedItem is ListViewItem item && item.Tag is string id)
            {
                selected = id;
                await RefreshDetailsAsync();
            }
        };
        testButton.Click += async (_, _) => await ExecuteAsync("checks", "Εκτέλεση ελέγχων και stress tests");
        buildButton.Click += async (_, _) => await ExecuteAsync("build", "Κατασκευή και έλεγχος πραγματικού EXE");
        setupButton.Click += async (_, _) => await ExecuteAsync("setup", "Δημιουργία προαιρετικού Setup");
        reportButton.Click += async (_, _) => await OpenReportAsync();
        folderButton.Click += async (_, _) => await OpenFolderAsync();
        trashButton.Click += async (_, _) => await TrashAsync();
        stopButton.Click += (_, _) => work?.Cancel();
        SetButtons();
        _ = RefreshProjectsAsync();
    }

    private UIElement BuildLayout()
    {
        var root = new Grid { Background = Background };
        root.RowDefinitions.Add(new RowDefinition { Height = new GridLength(94) });
        root.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        root.RowDefinitions.Add(new RowDefinition { Height = new GridLength(37) });

        var header = new Grid {
            Background = White, Padding = new Thickness(27, 13, 27, 13)
        };
        header.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        header.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        var mark = new Border {
            Background = Blue, Width = 57, Height = 57, CornerRadius = new CornerRadius(17),
            Child = new TextBlock {
                Text = "KP", Foreground = White, FontSize = 19,
                FontWeight = Microsoft.UI.Text.FontWeights.Bold,
                HorizontalAlignment = HorizontalAlignment.Center,
                VerticalAlignment = VerticalAlignment.Center
            },
            Shadow = new ThemeShadow(), Translation = new Vector3(0, 0, 18)
        };
        header.Children.Add(mark);
        var brand = new StackPanel { Margin = new Thickness(16, 1, 0, 0), Spacing = 2 };
        brand.Children.Add(Label("ΚΕΝΤΡΟ ΠΡΟΓΡΑΜΜΑΤΙΣΜΟΥ", 20, true));
        brand.Children.Add(Label("Προγράμματα · Έλεγχοι · Κατασκευή · Εκδόσεις", 13));
        Grid.SetColumn(brand, 1);
        header.Children.Add(brand);
        root.Children.Add(header);

        var content = Columns(303, 0);
        content.ColumnSpacing = 20;
        content.Margin = new Thickness(22, 24, 22, 18);
        Grid.SetRow(content, 1);
        root.Children.Add(content);

        var sidebar = new Grid();
        sidebar.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        sidebar.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        sidebar.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        sidebar.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        sidebar.RowSpacing = 13;
        sidebar.Children.Add(Label("ΤΑ ΠΡΟΓΡΑΜΜΑΤΑ ΜΟΥ", 16, true));
        Grid.SetRow(counter, 1);
        sidebar.Children.Add(counter);
        projects.BorderThickness = new Thickness(0);
        projects.Background = Color(248, 250, 255);
        projects.CornerRadius = new CornerRadius(14);
        Grid.SetRow(projects, 2);
        sidebar.Children.Add(projects);
        var sideActions = new StackPanel { Spacing = 9 };
        var import = ActionButton("＋  ΕΙΣΑΓΩΓΗ ΠΡΟΓΡΑΜΜΑΤΟΣ ZIP", true);
        import.Click += async (_, _) => await ImportAsync();
        sideActions.Children.Add(import);
        var reload = ActionButton("ΑΝΑΝΕΩΣΗ ΛΙΣΤΑΣ", false);
        reload.Click += async (_, _) => await RefreshProjectsAsync();
        sideActions.Children.Add(reload);
        var restore = ActionButton("ΕΠΑΝΑΦΟΡΑ ΑΦΑΙΡΕΜΕΝΟΥ", false);
        restore.Click += async (_, _) => await RestoreAsync();
        sideActions.Children.Add(restore);
        sideActions.Children.Add(trashButton);
        Grid.SetRow(sideActions, 3);
        sidebar.Children.Add(sideActions);
        content.Children.Add(Card(sidebar, new Thickness(16)));

        var right = new Grid();
        right.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        right.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        right.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        right.RowSpacing = 18;
        Grid.SetColumn(right, 1);
        content.Children.Add(right);

        var hero = new Grid { MinHeight = 146 };
        hero.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        hero.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        hero.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        hero.RowSpacing = 10;
        hero.Children.Add(Label("ΕΠΙΛΕΓΜΕΝΟ ΠΡΟΓΡΑΜΜΑ", 12, true));
        Grid.SetRow(projectName, 1);
        hero.Children.Add(projectName);
        Grid.SetRow(projectInfo, 2);
        hero.Children.Add(projectInfo);
        right.Children.Add(Card(hero, new Thickness(24, 21, 24, 21)));

        var phases = new Grid { ColumnSpacing = 10 };
        for (int i = 0; i < 4; i++) phases.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        phases.Children.Add(Phase("01", "ΕΛΕΓΧΟΙ", "Αυτόματοι έλεγχοι και πίεση", testButton, 0));
        phases.Children.Add(Phase("02", "ΑΝΑΦΟΡΑ", "Προβολή αποτελεσμάτων", reportButton, 1));
        phases.Children.Add(Phase("03", "ΠΡΟΓΡΑΜΜΑ EXE", "Μόνο μετά από επιτυχείς ελέγχους", buildButton, 2));
        phases.Children.Add(Phase("04", "ΕΓΚΑΤΑΣΤΑΣΗ", "Προαιρετικό Setup", setupButton, 3));
        Grid.SetRow(phases, 1);
        right.Children.Add(phases);

        var activity = new Grid();
        activity.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        activity.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        activity.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        activity.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        activity.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        activity.RowSpacing = 9;
        activity.Children.Add(logHeading);
        Grid.SetRow(stageText, 1);
        activity.Children.Add(stageText);
        Grid.SetRow(progress, 2);
        activity.Children.Add(progress);
        results.BorderThickness = new Thickness(0);
        results.Background = Color(248, 250, 254);
        results.CornerRadius = new CornerRadius(13);
        Grid.SetRow(results, 3);
        activity.Children.Add(results);
        var bottom = Columns(0, 0);
        bottom.ColumnSpacing = 12;
        Grid.SetColumn(folderButton, 0);
        Grid.SetColumn(stopButton, 1);
        bottom.Children.Add(folderButton);
        bottom.Children.Add(stopButton);
        Grid.SetRow(bottom, 4);
        activity.Children.Add(bottom);
        Grid.SetRow(activity, 2);
        right.Children.Add(Card(activity));

        var footer = new Grid { Margin = new Thickness(25, 4, 25, 4) };
        footer.Children.Add(status);
        Grid.SetRow(footer, 2);
        root.Children.Add(footer);
        return root;
    }
    private static Border Phase(string number, string title, string subtitle, Button action, int column)
    {
        var stack = new StackPanel { Spacing = 9 };
        stack.Children.Add(new TextBlock { Text = number, Foreground = Blue, FontSize = 19,
            FontWeight = Microsoft.UI.Text.FontWeights.Bold });
        stack.Children.Add(Label(title, 14, true));
        var note = Label(subtitle, 11.5);
        note.MinHeight = 35;
        stack.Children.Add(note);
        stack.Children.Add(action);
        var card = Card(stack, new Thickness(13, 17, 13, 15));
        Grid.SetColumn(card, column);
        return card;
    }

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
            Text = entry, FontFamily = new FontFamily("Segoe UI Variable"), FontSize = 13,
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
