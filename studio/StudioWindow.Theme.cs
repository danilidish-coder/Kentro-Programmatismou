using Microsoft.UI;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Media.Animation;
using System.Numerics;

namespace KentroStudio;

// Visual system: deep navy operational desktop, high-contrast Fluent cards,
// tactile blue gradient controls, and readable Greek labels.
internal sealed partial class StudioWindow
{
    private static Windows.UI.Color RGB(byte r, byte g, byte b) =>
        Windows.UI.Color.FromArgb(255, r, g, b);
    private static SolidColorBrush Color(byte r, byte g, byte b) => new(RGB(r, g, b));
    private static readonly SolidColorBrush Ink = Color(244, 249, 255);
    private static readonly SolidColorBrush Muted = Color(170, 198, 223);
    private static readonly SolidColorBrush Blue = Color(55, 174, 241);
    private static readonly SolidColorBrush Background = Color(5, 18, 30);
    private static readonly SolidColorBrush PaleBlue = Color(20, 58, 88);
    private static readonly SolidColorBrush White = Color(255, 255, 255);
    private static readonly SolidColorBrush CardOutline = Color(48, 114, 153);
    private static readonly SolidColorBrush FieldBackground = Color(6, 29, 47);

    private static LinearGradientBrush Gradient(
        (byte R, byte G, byte B) top, (byte R, byte G, byte B) bottom)
    {
        var gradient = new LinearGradientBrush
        {
            StartPoint = new Windows.Foundation.Point(0, 0),
            EndPoint = new Windows.Foundation.Point(0, 1)
        };
        gradient.GradientStops.Add(new GradientStop { Color = RGB(top.R, top.G, top.B), Offset = 0 });
        gradient.GradientStops.Add(new GradientStop { Color = RGB(bottom.R, bottom.G, bottom.B), Offset = 1 });
        return gradient;
    }

    private static readonly Brush PanelGradient = Gradient((16, 54, 80), (7, 29, 48));
    private static readonly Brush HeaderGradient = Gradient((22, 66, 98), (5, 21, 37));
    private static readonly Brush ActiveGradient = Gradient((50, 165, 236), (7, 91, 161));
    private static readonly Brush InactiveGradient = Gradient((22, 68, 102), (9, 35, 58));
    private static readonly Brush GoodGradient = Gradient((23, 166, 120), (8, 99, 72));

    private static TextBlock Label(string value, double size = 16, bool strong = false)
    {
        return new TextBlock
        {
            Text = value,
            FontFamily = new FontFamily("Segoe UI Variable"),
            FontSize = size,
            FontWeight = strong
                ? Microsoft.UI.Text.FontWeights.Bold
                : Microsoft.UI.Text.FontWeights.Normal,
            Foreground = strong ? Ink : Muted,
            TextWrapping = TextWrapping.Wrap
        };
    }

    private static Button ActionButton(string value, bool accent)
    {
        var normal = accent ? ActiveGradient : InactiveGradient;
        var b = new Button
        {
            Content = value,
            HorizontalAlignment = HorizontalAlignment.Stretch,
            Height = 51,
            FontFamily = new FontFamily("Segoe UI Variable"),
            FontSize = 14,
            FontWeight = Microsoft.UI.Text.FontWeights.Bold,
            Background = normal,
            Foreground = Ink,
            BorderBrush = accent ? Color(89, 194, 255) : CardOutline,
            BorderThickness = new Thickness(1.3),
            CornerRadius = new CornerRadius(10),
            Padding = new Thickness(8, 8, 8, 8)
        };
        b.Shadow = new ThemeShadow();
        b.Translation = new Vector3(0, 0, accent ? 20 : 10);
        b.PointerEntered += (_, _) =>
        {
            if (b.IsEnabled) b.Scale = new Vector3(1.012f, 1.012f, 1);
        };
        b.PointerExited += (_, _) => b.Scale = Vector3.One;
        b.PointerPressed += (_, _) => b.Scale = new Vector3(.985f, .985f, 1);
        b.PointerReleased += (_, _) => b.Scale = Vector3.One;
        return b;
    }

    private static Border Card(UIElement body, Thickness? pad = null)
    {
        return new Border
        {
            Background = PanelGradient,
            CornerRadius = new CornerRadius(13),
            BorderBrush = CardOutline,
            BorderThickness = new Thickness(1.2),
            Padding = pad ?? new Thickness(18),
            Child = body,
            Shadow = new ThemeShadow(),
            Translation = new Vector3(0, 0, 16)
        };
    }

    private static Grid Columns(params double[] widths)
    {
        var grid = new Grid();
        foreach (var width in widths)
            grid.ColumnDefinitions.Add(new ColumnDefinition
            {
                Width = width == 0
                    ? new GridLength(1, GridUnitType.Star)
                    : new GridLength(width)
            });
        return grid;
    }
}
