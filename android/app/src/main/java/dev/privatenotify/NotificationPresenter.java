package dev.privatenotify;

import android.Manifest;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.os.Build;
import android.media.AudioAttributes;
import android.media.RingtoneManager;

import org.json.JSONObject;

final class NotificationPresenter {
    private static final String CHANNEL_ID = "private_notify_messages";

    private NotificationPresenter() {
    }

    static void ensureChannel(Context context) {
        ensureChannel(context, "default");
    }

    private static void ensureChannel(Context context, String alert) {
        NotificationManager manager = context.getSystemService(NotificationManager.class);
        if (manager == null) {
            return;
        }

        manager.createNotificationChannel(newChannel(alert));
    }

    static String channelId(String alert) {
        return "default".equals(alert) ? CHANNEL_ID : CHANNEL_ID + "_" + alert + "_v1";
    }

    static NotificationChannel newChannel(String alert) {
        NotificationChannel channel = new NotificationChannel(channelId(alert),
                "default".equals(alert) ? "Private Notify" : MessageStyle.alertLabel(alert),
                "silent".equals(alert) ? NotificationManager.IMPORTANCE_LOW : NotificationManager.IMPORTANCE_HIGH);
        channel.setDescription("Encrypted notifications decrypted on this device");
        // Keep the original channel untouched. Android preserves user overrides on all channels.
        if (!"default".equals(alert)) {
            boolean sound = "sound".equals(alert) || "sound-vibrate".equals(alert);
            boolean vibrate = "vibrate".equals(alert) || "sound-vibrate".equals(alert);
            channel.setSound(sound ? RingtoneManager.getDefaultUri(RingtoneManager.TYPE_NOTIFICATION) : null,
                    new AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_NOTIFICATION)
                            .setContentType(AudioAttributes.CONTENT_TYPE_SONIFICATION).build());
            if (vibrate) channel.setVibrationPattern(new long[]{0, 250, 150, 250});
            channel.enableVibration(vibrate);
        }
        return channel;
    }

    static void show(Context context, JSONObject record) {
        if (Build.VERSION.SDK_INT >= 33
                && context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            return;
        }

        String alert = MessageStyle.alert(record);
        ensureChannel(context, alert);

        Intent intent = new Intent(context, MainActivity.class);
        intent.setFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP | Intent.FLAG_ACTIVITY_SINGLE_TOP);
        intent.putExtra("notificationId", record.optString("id"));
        PendingIntent pendingIntent = PendingIntent.getActivity(
                context,
                record.optString("id").hashCode(),
                intent,
                PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT
        );

        Notification.Builder builder = new Notification.Builder(context, channelId(alert))
                .setSmallIcon(R.drawable.ic_notify)
                .setContentTitle(record.optString("title", "Notification"))
                .setContentText(record.optString("body", record.optString("service", "Private Notify")))
                .setStyle(new Notification.BigTextStyle().bigText(record.optString("body", "")))
                .setContentIntent(pendingIntent)
                .setAutoCancel(true)
                .setShowWhen(true);
        if (!MessageStyle.icon(record).isEmpty() || !MessageStyle.severity(record).isEmpty()) {
            builder.setLargeIcon(MessageStyle.largeIcon(context, record));
            builder.setColor(MessageStyle.color(context, record));
        }
        Notification notification = builder.build();

        NotificationManager manager = context.getSystemService(NotificationManager.class);
        if (manager != null) {
            manager.notify(record.optString("id", String.valueOf(System.nanoTime())).hashCode(), notification);
        }
    }
}
