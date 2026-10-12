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
    private UIElement BuildLayout()
    {
        var root = new Grid { Background = Background };
        root.RowDefinitions.Add(new RowDefinition { Height = new GridLength(78) });
        root.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        root.RowDefinitions.Add(new RowDefinition { Height = new GridLength(28) });

        var header = new Grid {
            Background = White, Padding = new Thickness(22, 10, 22, 10)
        };
        header.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        header.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        var mark = new Border {
            Background = Blue, Width = 48, Height = 48, CornerRadius = new CornerRadius(14),
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
        brand.Children.Add(Label("ΚΕΝΤΡΟ ΠΡΟΓΡΑΜΜΑΤΙΣΜΟΥ", 18, true));
        brand.Children.Add(Label("Προγράμματα · Έλεγχοι · Κατασκευή · Εκδόσεις", 13));
        Grid.SetColumn(brand, 1);
        header.Children.Add(brand);
        root.Children.Add(header);

        var content = Columns(278, 0);
        content.ColumnSpacing = 14;
        content.Margin = new Thickness(16, 15, 16, 10);
        Grid.SetRow(content, 1);
        root.Children.Add(content);

        var sidebar = new Grid();
        sidebar.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        sidebar.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        sidebar.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        sidebar.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        sidebar.RowSpacing = 9;
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
        right.RowSpacing = 11;
        Grid.SetColumn(right, 1);
        content.Children.Add(right);

        var hero = new Grid();
        hero.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        hero.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        hero.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        hero.RowSpacing = 4;
        hero.Children.Add(Label("ΕΠΙΛΕΓΜΕΝΟ ΠΡΟΓΡΑΜΜΑ", 12, true));
        Grid.SetRow(projectName, 1);
        hero.Children.Add(projectName);
        Grid.SetRow(projectInfo, 2);
        hero.Children.Add(projectInfo);
        var heroCard = Card(hero, new Thickness(18, 12, 18, 12));
        Grid.SetRow(heroCard, 0);
        right.Children.Add(heroCard);

        var phases = new Grid { ColumnSpacing = 8 };
        for (int i = 0; i < 4; i++) phases.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        phases.Children.Add(Phase("01", "ΕΛΕΓΧΟΙ", "Αυτόματοι έλεγχοι και πίεση", testButton, 0));
        phases.Children.Add(Phase("02", "ΑΝΑΦΟΡΑ", "Προβολή αποτελεσμάτων", reportButton, 1));
        phases.Children.Add(Phase("03", "ΠΡΟΓΡΑΜΜΑ EXE", "Μετά από επιτυχείς ελέγχους", buildButton, 2));
        phases.Children.Add(Phase("04", "ΕΓΚΑΤΑΣΤΑΣΗ", "Προαιρετικό Setup", setupButton, 3));
        Grid.SetRow(phases, 1);
        right.Children.Add(phases);

        var activity = new Grid();
        activity.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        activity.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        activity.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        activity.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        activity.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        activity.RowSpacing = 6;
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
        var activityCard = Card(activity, new Thickness(16, 13, 16, 13));
        Grid.SetRow(activityCard, 2);
        right.Children.Add(activityCard);

        var footer = new Grid { Margin = new Thickness(25, 4, 25, 4) };
        footer.Children.Add(status);
        Grid.SetRow(footer, 2);
        root.Children.Add(footer);
        return root;
    }
    private static Border Phase(string number, string title, string subtitle, Button action, int column)
    {
        var stack = new StackPanel { Spacing = 5 };
        stack.Children.Add(new TextBlock { Text = number, Foreground = Blue, FontSize = 17,
            FontWeight = Microsoft.UI.Text.FontWeights.Bold });
        stack.Children.Add(Label(title, 12, true));
        var note = Label(subtitle, 10.5);
        note.MinHeight = 31;
        stack.Children.Add(note);
        action.Height = 38;
        stack.Children.Add(action);
        var card = Card(stack, new Thickness(10, 11, 10, 10));
        Grid.SetColumn(card, column);
        return card;
    }
}
