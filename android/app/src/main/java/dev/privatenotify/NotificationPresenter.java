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

import org.json.JSONObject;

final class NotificationPresenter {
    private static final String CHANNEL_ID = "private_notify_messages";

    private NotificationPresenter() {
    }

    static void ensureChannel(Context context) {
        NotificationManager manager = context.getSystemService(NotificationManager.class);
        if (manager == null) {
            return;
        }

        NotificationChannel channel = new NotificationChannel(
                CHANNEL_ID,
                "Private Notify",
                NotificationManager.IMPORTANCE_HIGH
        );
        channel.setDescription("Encrypted notifications decrypted on this device");
        manager.createNotificationChannel(channel);
    }

    static void show(Context context, JSONObject record) {
        if (Build.VERSION.SDK_INT >= 33
                && context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            return;
        }

        ensureChannel(context);

        Intent intent = new Intent(context, MainActivity.class);
        intent.setFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP | Intent.FLAG_ACTIVITY_SINGLE_TOP);
        PendingIntent pendingIntent = PendingIntent.getActivity(
                context,
                0,
                intent,
                PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT
        );

        Notification notification = new Notification.Builder(context, CHANNEL_ID)
                .setSmallIcon(R.drawable.ic_notify)
                .setContentTitle(record.optString("title", "Notification"))
                .setContentText(record.optString("body", record.optString("service", "Private Notify")))
                .setStyle(new Notification.BigTextStyle().bigText(record.optString("body", "")))
                .setContentIntent(pendingIntent)
                .setAutoCancel(true)
                .setShowWhen(true)
                .build();

        NotificationManager manager = context.getSystemService(NotificationManager.class);
        if (manager != null) {
            manager.notify(record.optString("id", String.valueOf(System.nanoTime())).hashCode(), notification);
        }
    }
}
