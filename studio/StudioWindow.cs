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

// Native desktop host: fields and wiring only; layout, theme and actions live in separate modules.
internal sealed partial class StudioWindow : Window
{
    private readonly ListView projects = new();
    private readonly ListView results = new();
    private readonly TextBlock projectName = Label("Δεν έχει επιλεγεί πρόγραμμα", 25, true);
    private readonly TextBlock projectInfo = Label("Εισάγετε ZIP για να ξεκινήσετε.", 14);
    private readonly TextBlock stageText = Label("Αναμονή εισαγωγής έργου", 14, true);
    private readonly TextBlock logHeading = Label("Αποτελέσματα εργασίας", 18, true);
    private readonly ProgressBar progress = new() { IsIndeterminate = true, Visibility = Visibility.Collapsed, Height = 4 };
    private readonly Button testButton = ActionButton("ΕΛΕΓΧΟΙ", true);
    private readonly Button buildButton = ActionButton("EXE", true);
    private readonly Button setupButton = ActionButton("SETUP", false);
    private readonly Button reportButton = ActionButton("ΑΝΑΦΟΡΑ", false);
    private readonly Button folderButton = ActionButton("ΦΑΚΕΛΟΣ ΠΡΟΓΡΑΜΜΑΤΟΣ", false);
    private readonly Button trashButton = ActionButton("ΑΦΑΙΡΕΣΗ", false);
    private readonly Button stopButton = ActionButton("ΑΚΥΡΩΣΗ", false);
    private readonly TextBlock counter = Label("0 ΕΡΓΑ", 13, true);
    private readonly TextBlock status = Label("Τοπική εργασία · Χωρίς εξάρτηση από GitHub", 12);
    private CancellationTokenSource? work;
    private string? selected;
    private bool busy;

    public StudioWindow()
    {
        Title = "Κέντρο Προγραμματισμού · Studio";
        SystemBackdrop = new MicaBackdrop();
        var display = Microsoft.UI.Windowing.DisplayArea.GetFromWindowId(
            AppWindow.Id, Microsoft.UI.Windowing.DisplayAreaFallback.Primary);
        // Keep the entire decorated window on screen (including resize borders).
        var width = Math.Min(1210, Math.Max(640, display.WorkArea.Width - 80));
        var height = Math.Min(810, Math.Max(480, display.WorkArea.Height - 80));
        var left = display.WorkArea.X + Math.Max(0, (display.WorkArea.Width - width) / 2);
        var top = display.WorkArea.Y + Math.Max(0, (display.WorkArea.Height - height) / 2);
        AppWindow.MoveAndResize(new Windows.Graphics.RectInt32(left, top, width, height));
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
        var integrationZip = Environment.GetEnvironmentVariable("KENTRO_STUDIO_INTEGRATION_ZIP");
        if (!string.IsNullOrWhiteSpace(integrationZip))
            _ = RunCiIntegrationAsync(integrationZip);
    }
}
