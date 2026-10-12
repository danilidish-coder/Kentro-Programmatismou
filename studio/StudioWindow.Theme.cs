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
}
