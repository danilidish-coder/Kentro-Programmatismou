using System.Windows;
using System.Windows.Controls;
namespace KentroTextDiagnostic;
public partial class MainWindow : Window
{
    public MainWindow() => InitializeComponent();
    private void TestButton_Click(object sender, RoutedEventArgs e)
    {
        var mode = (sender as Button)?.Tag?.ToString() ?? "Άγνωστο";
        Status.Text = "Το κουμπί " + mode + " λειτουργεί. Έλεγξε αν το κείμενο είναι καθαρό πριν και μετά το πάτημα.";
    }
}
