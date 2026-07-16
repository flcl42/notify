package dev.privatenotify;

import android.content.Context;

import com.google.firebase.messaging.FirebaseMessaging;

import org.json.JSONObject;

import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

final class PairingRegistrar {
    interface Callback {
        void onSuccess(JSONObject subscription);

        void onError(Exception error);
    }

    private PairingRegistrar() {
    }

    static void register(Context context, JSONObject pairing, Callback callback) throws Exception {
        if (!BuildConfig.FIREBASE_CONFIGURED) {
            throw new IllegalStateException("Firebase config is missing.");
        }

        Context appContext = context.getApplicationContext();
        FirebaseMessaging.getInstance().getToken().addOnCompleteListener(task -> {
            if (!task.isSuccessful() || task.getResult() == null) {
                callback.onError(new IllegalStateException("Could not get FCM token."));
                return;
            }

            String token = task.getResult();
            new NotifyStore(appContext).setFcmToken(token);
            ExecutorService background = Executors.newSingleThreadExecutor();
            background.execute(() -> {
                try {
                    JSONObject subscription = registerTokenWithCli(appContext, pairing, token);
                    callback.onSuccess(subscription);
                } catch (Exception error) {
                    callback.onError(error);
                } finally {
                    background.shutdown();
                }
            });
        });
    }

    private static JSONObject registerTokenWithCli(Context context, JSONObject pairing, String token) throws Exception {
        JSONObject body = new JSONObject();
        body.put("subscriptionId", pairing.getString("subscriptionId"));
        body.put("provider", "fcm");
        body.put("pushToken", token);
        body.put("platform", "android");

        byte[] bytes = body.toString().getBytes(StandardCharsets.UTF_8);
        URL url = new URL(pairing.getString("registrationUrl"));
        HttpURLConnection connection = (HttpURLConnection) url.openConnection();
        connection.setRequestMethod("POST");
        connection.setConnectTimeout(10000);
        connection.setReadTimeout(10000);
        connection.setDoOutput(true);
        connection.setRequestProperty("content-type", "application/json; charset=utf-8");
        try (OutputStream output = connection.getOutputStream()) {
            output.write(bytes);
        }

        int statusCode = connection.getResponseCode();
        if (statusCode < 200 || statusCode >= 300) {
            throw new IllegalStateException("CLI registration returned HTTP " + statusCode);
        }

        JSONObject subscription = new JSONObject();
        subscription.put("id", pairing.getString("subscriptionId"));
        subscription.put("name", pairing.optString("name", "phone"));
        subscription.put("defaultTitle", pairing.optString("defaultTitle", pairing.optString("name", "Notification")));
        subscription.put("delivery", "push");
        subscription.put("registrationUrl", pairing.getString("registrationUrl"));
        subscription.put("key", pairing.getString("key"));
        subscription.put("pushToken", token);
        subscription.put("createdAt", pairing.optString("createdAt", Instant.now().toString()));
        new NotifyStore(context).upsertSubscription(subscription);
        return subscription;
    }
}
