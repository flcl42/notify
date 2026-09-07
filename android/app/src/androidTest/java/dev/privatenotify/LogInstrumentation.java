package dev.privatenotify;

import android.app.Activity;
import android.app.Instrumentation;
import android.content.Context;
import android.content.ContextWrapper;
import android.content.SharedPreferences;
import android.os.Bundle;
import org.json.JSONObject;

/** Tests storage and display metadata without touching a user's saved sources. */
public final class LogInstrumentation extends Instrumentation {
    private Bundle args;
    @Override public void onCreate(Bundle arguments) { args = arguments; start(); }
    @Override public void onStart() {
        Bundle result = new Bundle();
        try {
            android.content.pm.ActivityInfo scanner = getTargetContext().getPackageManager().getActivityInfo(
                    new android.content.ComponentName(getTargetContext(), com.journeyapps.barcodescanner.CaptureActivity.class), 0);
            check(scanner.screenOrientation == android.content.pm.ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED,
                    "QR scanner must not force landscape");
            Context isolated = new ContextWrapper(getTargetContext()) {
                @Override public SharedPreferences getSharedPreferences(String name, int mode) {
                    return super.getSharedPreferences("log_test_" + name, mode);
                }
            };
            isolated.getSharedPreferences("private_notify", 0).edit().clear().commit();
            NotifyStore store = new NotifyStore(isolated);
            JSONObject subscription = new JSONObject().put("id", "test-source").put("name", "Builds").put("key", "retained-key");
            store.upsertSubscription(subscription);
            JSONObject plain = new JSONObject().put("id", "old").put("body", "Plain message");
            isolated.getSharedPreferences("private_notify", 0).edit()
                    .putString("notifications", new org.json.JSONArray().put(plain).toString()).commit();
            check(store.notifications().getJSONObject(0).getString("id").equals("old"), "Legacy log did not load");
            JSONObject note = new JSONObject().put("id", "new").put("body", "Unicode: \u4f60\u597d \u041f\u0440\u0438\u0432\u0435\u0442")
                    .put("icon", "\uD83D\uDC69\uD83C\uDFFD\u200D\uD83D\uDCBB").put("severity", "emergency").put("alert", "silent");
            JSONObject record = store.addNotification(subscription, note);
            check(record.getString("icon").equals(note.getString("icon")), "Unicode icon lost");
            check(record.getString("body").equals(note.getString("body")), "Unicode body lost");
            check(record.getString("alert").equals("silent"), "Alert mode lost");
            check(MessageStyle.alert(plain).equals("default"), "Legacy alert behavior changed");
            check(MessageStyle.alert(new JSONObject().put("alert", "unknown")).equals("default"), "Unknown alert must use default");
            testAlertChannels();
            testThemes();
            check(MessageStyle.severity(plain).isEmpty(), "Old records need no migration");
            check(MessageStyle.color(isolated, record) != MessageStyle.color(isolated, plain), "Severity color missing");
            check(MessageStyle.largeIcon(isolated, record).getWidth() == 128, "Notification bitmap missing");
            store.markRead("new");
            check(store.notifications().getJSONObject(0).getBoolean("read"), "Read flag lost");
            check(!store.notifications().getJSONObject(1).optBoolean("read"), "Wrong message marked read");
            store.deleteNotification("new");
            check(store.notifications().length() == 1, "Individual delete failed");
            testConcurrentWrites(isolated, subscription);
            store.clearNotifications();
            check(store.subscriptionById("test-source").getString("key").equals("retained-key"), "Log actions changed registered key");
            check(MessageStyle.icon(new JSONObject().put("icon", "a\nb")).isEmpty(), "Multiline icon accepted");
            isolated.getSharedPreferences("private_notify", 0).edit().clear().commit();
            if (args != null && args.getString("demo", "false").equals("true")) seedDemo();
            result.putString("stream", "PASS: light/dark contrast, Unicode, severity, alert channels, old records, read/delete, registered-key preservation\n");
            finish(Activity.RESULT_OK, result);
        } catch (Exception | AssertionError error) {
            result.putString("stream", "FAIL: " + error.toString()); finish(Activity.RESULT_CANCELED, result);
        }
    }
    private void check(boolean value, String message) { if (!value) throw new AssertionError(message); }
    private void testThemes() throws Exception {
        for (int mode : new int[]{android.content.res.Configuration.UI_MODE_NIGHT_NO, android.content.res.Configuration.UI_MODE_NIGHT_YES}) {
            android.content.res.Configuration config = new android.content.res.Configuration(getTargetContext().getResources().getConfiguration());
            config.uiMode = (config.uiMode & ~android.content.res.Configuration.UI_MODE_NIGHT_MASK) | mode;
            Context context = getTargetContext().createConfigurationContext(config);
            int background = context.getColor(R.color.log_background);
            boolean dark = mode == android.content.res.Configuration.UI_MODE_NIGHT_YES;
            check((luminance(background) < 0.1) == dark, "Wrong background for night mode");
            for (int resource : new int[]{R.color.log_text, R.color.log_muted, R.color.log_accent}) {
                check(contrast(context.getColor(resource), background) >= 4.5, "Unreadable log text");
                check(contrast(context.getColor(resource), context.getColor(R.color.log_search)) >= 4.5, "Unreadable search text");
            }
            for (String severity : new String[]{"", "info", "success", "warning", "error", "emergency"}) {
                int color = MessageStyle.color(context, new JSONObject().put("severity", severity));
                check(contrast(color, background) >= 4.5, "Unreadable severity: " + severity);
            }
            android.view.ContextThemeWrapper themed = new android.view.ContextThemeWrapper(context, R.style.AppTheme);
            android.util.TypedValue bars = new android.util.TypedValue();
            themed.getTheme().resolveAttribute(android.R.attr.windowLightStatusBar, bars, true);
            check((bars.data != 0) != dark, "Status bar does not match theme");
        }
    }
    private double luminance(int color) {
        double sum = 0;
        double[] weights = {0.2126, 0.7152, 0.0722};
        for (int i = 0; i < 3; i++) {
            double channel = ((color >> (16 - 8 * i)) & 255) / 255.0;
            sum += weights[i] * (channel <= 0.04045 ? channel / 12.92 : Math.pow((channel + 0.055) / 1.055, 2.4));
        }
        return sum;
    }
    private double contrast(int a, int b) {
        double l1 = luminance(a), l2 = luminance(b);
        return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05);
    }
    private void testAlertChannels() {
        java.util.HashSet<String> ids = new java.util.HashSet<>();
        for (String mode : new String[]{"default", "sound", "vibrate", "sound-vibrate", "silent"}) {
            android.app.NotificationChannel channel = NotificationPresenter.newChannel(mode);
            check(ids.add(channel.getId()), "Alert modes share a channel");
            if (mode.equals("default")) {
                check(channel.getId().equals("private_notify_messages"), "Existing channel changed");
                continue;
            }
            check((channel.getSound() != null) == mode.contains("sound"), "Wrong sound behavior: " + mode);
            check(channel.shouldVibrate() == mode.contains("vibrate"), "Wrong vibration behavior: " + mode);
            check(channel.getImportance() == (mode.equals("silent") ? 2 : 4), "Wrong importance: " + mode);
            check(!channel.canBypassDnd(), "Alert mode bypasses DND");
        }
    }
    private void testConcurrentWrites(Context context, JSONObject subscription) throws Exception {
        java.util.concurrent.atomic.AtomicReference<Throwable> failure = new java.util.concurrent.atomic.AtomicReference<>();
        Thread[] writers = new Thread[3];
        for (int i = 0; i < writers.length; i++) {
            final int worker = i;
            writers[i] = new Thread(() -> {
                try {
                    NotifyStore store = new NotifyStore(context);
                    for (int j = 0; j < 50; j++) {
                        if (worker == 2) store.markRead(null);
                        else store.addNotification(subscription, new JSONObject().put("id", worker + "-" + j));
                    }
                } catch (Throwable error) { failure.set(error); }
            });
            writers[i].start();
        }
        for (Thread writer : writers) writer.join();
        check(failure.get() == null, "Concurrent writer failed: " + failure.get());
        check(new NotifyStore(context).notifications().length() == 101, "Concurrent log updates lost a notification");
    }
    private void seedDemo() throws Exception {
        if (!android.os.Build.MODEL.contains("sdk")) throw new IllegalStateException("Demo data is emulator-only");
        NotifyStore store = new NotifyStore(getTargetContext());
        String[] levels = {"", "info", "success", "warning", "emergency"};
        String[] titles = {"Nightly backup", "Team update", "Deployment complete", "Storage running low", "Production incident"};
        String[] bodies = {"Archive uploaded. All files are up to date.", "Release review moved to 14:30. Notes are ready for the team.", "Version 2.8.1 is live. All health checks passed.", "Only 8% disk space remains on the build server.", "API response time exceeded 3 seconds. On-call engineer notified.\n\nRegion: eu-central\nStatus: investigating"};
        String[] icons = {"", "\u2139\uFE0F", "\u2705", "\u26A0\uFE0F", "\uD83D\uDEA8"};
        for (int i = 0; i < levels.length; i++) {
            JSONObject sub = new JSONObject().put("id", "demo-" + i).put("name", i == 4 ? "Monitoring" : i == 3 ? "Infrastructure" : "Workspace");
            JSONObject note = new JSONObject().put("id", "demo-" + i).put("title", titles[i]).put("body", bodies[i]).put("severity", levels[i]).put("icon", icons[i]);
            note.put("alert", args.getString("alert", "default"));
            JSONObject record = store.addNotification(sub, note);
            if (i == levels.length - 1) NotificationPresenter.show(getTargetContext(), record);
        }
        getTargetContext().getSharedPreferences("private_notify", 0).edit().commit();
    }
}
