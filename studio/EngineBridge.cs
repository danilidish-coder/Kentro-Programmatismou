using System.Diagnostics;
using System.Text.Json;

namespace KentroStudio;

// Local-only process bridge. No network, no GitHub dependency, no shell commands.
internal static class EngineBridge
{
    public static async Task<JsonElement> RunAsync(string action, string? value = null,
        Action<string>? progress = null, CancellationToken cancel = default)
    {
        var executable = Path.Combine(AppContext.BaseDirectory, "Kentro_Engine.exe");
        if (!File.Exists(executable))
            throw new FileNotFoundException("Δεν βρέθηκε η τοπική μηχανή εκτέλεσης.", executable);

        var start = new ProcessStartInfo(executable)
        {
            UseShellExecute = false,
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            CreateNoWindow = true,
            WorkingDirectory = AppContext.BaseDirectory
        };
        start.ArgumentList.Add(action);
        if (value is not null) start.ArgumentList.Add(value);

        using var process = new Process { StartInfo = start };
        if (!process.Start()) throw new InvalidOperationException("Δεν ξεκίνησε η μηχανή δοκιμών.");
        using var registration = cancel.Register(() =>
        {
            try { if (!process.HasExited) process.Kill(entireProcessTree: true); }
            catch (InvalidOperationException) { }
        });

        var stderr = process.StandardError.ReadToEndAsync(cancel);
        JsonElement result = default;
        var seenResult = false;
        string? failure = null;
        while (true)
        {
            var line = await process.StandardOutput.ReadLineAsync(cancel);
            if (line is null) break;
            using var doc = JsonDocument.Parse(line);
            var root = doc.RootElement;
            if (!root.TryGetProperty("type", out var type)) continue;
            switch (type.GetString())
            {
                case "progress":
                    if (root.TryGetProperty("message", out var msg)) progress?.Invoke(msg.GetString() ?? "");
                    break;
                case "error":
                    failure = root.TryGetProperty("message", out var err) ? err.GetString() : "Αποτυχία";
                    break;
                case "result":
                    if (root.TryGetProperty("payload", out var payload))
                    {
                        result = payload.Clone();
                        seenResult = true;
                    }
                    break;
            }
        }
        await process.WaitForExitAsync(cancel);
        if (failure is not null) throw new InvalidOperationException(failure);
        if (process.ExitCode != 0)
            throw new InvalidOperationException("Αποτυχία μηχανής: " + await stderr);
        if (!seenResult) throw new InvalidOperationException("Η μηχανή δεν επέστρεψε αποτέλεσμα.");
        return result;
    }
}
