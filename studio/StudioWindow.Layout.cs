using Microsoft.UI;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Media.Animation;
using System.Numerics;

namespace KentroStudio;

internal sealed partial class StudioWindow
{
    // Layout is independent of execution, project state and the bridge.
    // Large Greek labels and navy cards follow the working Control Center reference.
    private UIElement BuildLayout()
    {
        var root = new Grid { Background = Background, RequestedTheme = ElementTheme.Dark };
        root.RowDefinitions.Add(new RowDefinition { Height = new GridLength(116) });
        root.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        root.RowDefinitions.Add(new RowDefinition { Height = new GridLength(31) });

        var header = new Border
        {
            Background = HeaderGradient,
            BorderBrush = CardOutline,
            BorderThickness = new Thickness(0, 0, 0, 2),
            Padding = new Thickness(23, 13, 23, 13),
            Child = CreateHeader()
        };
        root.Children.Add(header);

        var content = Columns(295, 0);
        content.ColumnSpacing = 13;
        content.Margin = new Thickness(13, 12, 13, 9);
        Grid.SetRow(content, 1);
        root.Children.Add(content);

        var sidebar = new Grid { RowSpacing = 11 };
        sidebar.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        sidebar.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        sidebar.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        var sideTitle = new StackPanel { Spacing = 4 };
        sideTitle.Children.Add(Label("ΤΑ ΠΡΟΓΡΑΜΜΑΤΑ ΜΟΥ", 19, true));
        sideTitle.Children.Add(counter);
        sidebar.Children.Add(sideTitle);
        projects.BorderThickness = new Thickness(1);
        projects.BorderBrush = CardOutline;
        projects.Background = FieldBackground;
        projects.Foreground = Ink;
        projects.CornerRadius = new CornerRadius(8);
        Grid.SetRow(projects, 1);
        sidebar.Children.Add(projects);

        var sideActions = new StackPanel { Spacing = 10 };
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
        Grid.SetRow(sideActions, 2);
        sidebar.Children.Add(sideActions);
        content.Children.Add(Card(sidebar, new Thickness(15)));

        var heightConstrained = AppWindow.Size.Height < 760;
        var right = new Grid { RowSpacing = 13 };
        right.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        right.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        right.RowDefinitions.Add(new RowDefinition {
            Height = heightConstrained ? GridLength.Auto : new GridLength(1, GridUnitType.Star)
        });

        var hero = new Grid { RowSpacing = 7 };
        hero.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        hero.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        hero.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        hero.Children.Add(Label("ΕΠΙΛΕΓΜΕΝΟ ΠΡΟΓΡΑΜΜΑ", 14, true));
        Grid.SetRow(projectName, 1);
        hero.Children.Add(projectName);
        Grid.SetRow(projectInfo, 2);
        hero.Children.Add(projectInfo);
        var heroCard = Card(hero, new Thickness(20, 13, 20, 15));
        Grid.SetRow(heroCard, 0);
        right.Children.Add(heroCard);

        var compact = AppWindow.Size.Width < 810;
        var narrowTitles = AppWindow.Size.Width < 1080;
        var phases = new Grid { ColumnSpacing = 12, RowSpacing = 10 };
        for (var i = 0; i < (compact ? 2 : 4); i++)
            phases.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        if (compact)
        {
            phases.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
            phases.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        }
        var phaseCards = new[]
        {
            Phase("01", "ΕΛΕΓΧΟΙ", "Έλεγχοι και δοκιμές πίεσης", testButton),
            Phase("02", "ΑΝΑΦΟΡΑ", "Αναλυτικά αποτελέσματα", reportButton),
            Phase("03", "ΚΑΤΑΣΚΕΥΗ EXE", "Μόνο μετά τους ελέγχους", buildButton),
            Phase("04", narrowTitles ? "SETUP" : "ΕΓΚΑΤΑΣΤΑΣΗ", "Προαιρετικό Setup", setupButton)
        };
        for (var i = 0; i < phaseCards.Length; i++)
        {
            Grid.SetColumn(phaseCards[i], compact ? i % 2 : i);
            if (compact) Grid.SetRow(phaseCards[i], i / 2);
            phases.Children.Add(phaseCards[i]);
        }
        Grid.SetRow(phases, 1);
        right.Children.Add(phases);

        var activity = new Grid { RowSpacing = 10 };
        activity.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        activity.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        activity.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        activity.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        activity.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        activity.Children.Add(logHeading);
        Grid.SetRow(stageText, 1);
        activity.Children.Add(stageText);
        Grid.SetRow(progress, 2);
        activity.Children.Add(progress);
        results.BorderThickness = new Thickness(1);
        results.BorderBrush = CardOutline;
        results.Background = FieldBackground;
        results.Foreground = Ink;
        results.CornerRadius = new CornerRadius(8);
        if (heightConstrained) results.MinHeight = 210;
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
        var activityCard = Card(activity, new Thickness(17, 15, 17, 15));
        Grid.SetRow(activityCard, 2);
        right.Children.Add(activityCard);

        if (heightConstrained)
        {
            // On 1024x768 and smaller, stage controls and results stay reachable by scrolling.
            var view = new ScrollViewer
            {
                Content = right,
                HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled,
                VerticalScrollBarVisibility = ScrollBarVisibility.Auto
            };
            Grid.SetColumn(view, 1);
            content.Children.Add(view);
        }
        else
        {
            Grid.SetColumn(right, 1);
            content.Children.Add(right);
        }

        var footer = new Grid { Margin = new Thickness(18, 4, 18, 3) };
        footer.Children.Add(status);
        Grid.SetRow(footer, 2);
        root.Children.Add(footer);
        return root;
    }

    private static Grid CreateHeader()
    {
        var header = Columns(75, 0);
        var mark = new Border
        {
            Width = 66, Height = 66,
            Background = ActiveGradient,
            BorderBrush = Blue,
            BorderThickness = new Thickness(2),
            CornerRadius = new CornerRadius(13),
            Child = new TextBlock
            {
                Text = "ΚΠ", Foreground = Ink, FontSize = 27,
                FontWeight = Microsoft.UI.Text.FontWeights.Bold,
                HorizontalAlignment = HorizontalAlignment.Center,
                VerticalAlignment = VerticalAlignment.Center
            }
        };
        header.Children.Add(mark);
        var words = new StackPanel
        {
            VerticalAlignment = VerticalAlignment.Center,
            Spacing = 4,
            Margin = new Thickness(12, 0, 0, 0)
        };
        words.Children.Add(Label("ΚΕΝΤΡΟ ΠΡΟΓΡΑΜΜΑΤΙΣΜΟΥ", 27, true));
        words.Children.Add(Label("ΤΟΠΙΚΗ ΑΝΑΠΤΥΞΗ ΠΡΟΓΡΑΜΜΑΤΩΝ   •   ΕΛΕΓΧΟΙ   •   EXE   •   ΕΚΔΟΣΕΙΣ", 13, true));
        Grid.SetColumn(words, 1);
        header.Children.Add(words);
        return header;
    }

    private static Border Phase(string number, string title, string subtitle, Button action)
    {
        var stack = new StackPanel { Spacing = 6 };
        stack.Children.Add(new TextBlock
        {
            Text = number, Foreground = Blue, FontSize = 19,
            FontWeight = Microsoft.UI.Text.FontWeights.Bold
        });
        stack.Children.Add(Label(title, 16, true));
        var note = Label(subtitle, 12);
        note.MinHeight = 34;
        stack.Children.Add(note);
        action.Height = 48;
        stack.Children.Add(action);
        return Card(stack, new Thickness(14, 12, 14, 13));
    }
}
