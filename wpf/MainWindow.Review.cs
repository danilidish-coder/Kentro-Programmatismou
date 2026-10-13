using System;
using System.Threading.Tasks;
using System.Windows;
using Microsoft.Win32;

namespace KentroWpf;

public partial class MainWindow
{
    // The decision file comes from the conversation after the user has supplied
    // the full checks report. There is no AI or network API in this application.
    private async Task<bool> ImportReviewAsync(string path, bool interactive)
    {
        if (_busy || string.IsNullOrWhiteSpace(_selected)) return false;
        var id = _selected;
        try
        {
            var result = await EngineBridge.RunAsync("review-import", id, extra: path);
            if (Value(result, "status") != "APPROVED")
                throw new InvalidOperationException("Η έγκριση δεν καταχωρίστηκε.");
            AddLog("Καταχωρίστηκε έγκριση για τη συγκεκριμένη έκδοση και τα συγκεκριμένα τεστ.");
            await RefreshDetailsAsync();
            return true;
        }
        catch (Exception ex)
        {
            App.RecordError("Review import", ex);
            if (_integration) throw;
            if (interactive)
                MessageBox.Show(this, ex.Message,
                    "Η έγκριση δεν έγινε δεκτή", MessageBoxButton.OK, MessageBoxImage.Warning);
            return false;
        }
    }

    private async void ReviewImport_Click(object sender, RoutedEventArgs e)
    {
        if (_busy || _selected is null) return;
        var dialog = new OpenFileDialog
        {
            Title = "Φόρτωση έγκρισης ChatGPT για κατασκευή EXE",
            Filter = "Έγγραφο έγκρισης (*.json)|*.json",
            CheckFileExists = true,
            Multiselect = false
        };
        if (dialog.ShowDialog(this) == true)
            await ImportReviewAsync(dialog.FileName, true);
    }
}
