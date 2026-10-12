using Microsoft.UI.Xaml;

namespace KentroStudio;

public partial class App : Application
{
    private Window? _window;
    public App()
    {
        InitializeComponent();
    }
    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        _window = new StudioWindow();
        _window.Activate();
    }
}
