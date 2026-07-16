package dev.privatenotify;

import android.content.Context;
import android.content.SharedPreferences;

import org.json.JSONArray;
import org.json.JSONObject;

import java.time.Instant;

final class NotifyStore {
    private static final String PREFS = "private_notify";
    private static final String SUBSCRIPTIONS = "subscriptions";
    private static final String NOTIFICATIONS = "notifications";
    private static final String FCM_TOKEN = "fcm_token";
    private final SharedPreferences prefs;

    NotifyStore(Context context) {
        this.prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
    }

    JSONArray subscriptions() {
        return readArray(SUBSCRIPTIONS);
    }

    JSONArray notifications() {
        return readArray(NOTIFICATIONS);
    }

    JSONObject subscriptionById(String id) {
        JSONArray items = subscriptions();
        for (int index = 0; index < items.length(); index += 1) {
            JSONObject item = items.optJSONObject(index);
            if (item != null && id.equals(item.optString("id"))) {
                return item;
            }
        }
        return null;
    }

    void upsertSubscription(JSONObject subscription) throws Exception {
        JSONArray items = subscriptions();
        JSONArray next = new JSONArray();
        boolean replaced = false;

        for (int index = 0; index < items.length(); index += 1) {
            JSONObject item = items.getJSONObject(index);
            if (subscription.getString("id").equals(item.optString("id"))) {
                next.put(subscription);
                replaced = true;
            } else {
                next.put(item);
            }
        }

        if (!replaced) {
            next.put(subscription);
        }

        writeArray(SUBSCRIPTIONS, next);
    }

    void removeSubscription(String id) throws Exception {
        JSONArray items = subscriptions();
        JSONArray next = new JSONArray();

        for (int index = 0; index < items.length(); index += 1) {
            JSONObject item = items.getJSONObject(index);
            if (!id.equals(item.optString("id"))) {
                next.put(item);
            }
        }

        writeArray(SUBSCRIPTIONS, next);
    }

    JSONObject addNotification(JSONObject subscription, JSONObject decrypted) throws Exception {
        JSONObject record = new JSONObject();
        record.put("id", decrypted.optString("id", subscription.optString("id") + "-" + System.nanoTime()));
        record.put("subscriptionId", subscription.getString("id"));
        record.put("subscriptionName", subscription.optString("name", "phone"));
        record.put("service", decrypted.optString("service", "service"));
        record.put("title", decrypted.optString("title", subscription.optString("defaultTitle", "Notification")));
        record.put("body", decrypted.optString("body", ""));
        record.put("createdAt", decrypted.optString("createdAt", Instant.now().toString()));
        record.put("receivedAt", Instant.now().toString());

        JSONArray current = notifications();
        JSONArray next = new JSONArray();
        next.put(record);

        for (int index = 0; index < current.length() && next.length() < 200; index += 1) {
            JSONObject item = current.getJSONObject(index);
            if (!record.getString("id").equals(item.optString("id"))) {
                next.put(item);
            }
        }

        writeArray(NOTIFICATIONS, next);
        return record;
    }

    void clearNotifications() {
        prefs.edit().remove(NOTIFICATIONS).apply();
    }

    void setFcmToken(String token) {
        prefs.edit().putString(FCM_TOKEN, token).apply();
    }

    String fcmToken() {
        return prefs.getString(FCM_TOKEN, "");
    }

    private JSONArray readArray(String key) {
        try {
            return new JSONArray(prefs.getString(key, "[]"));
        } catch (Exception ignored) {
            return new JSONArray();
        }
    }

    private void writeArray(String key, JSONArray array) {
        prefs.edit().putString(key, array.toString()).apply();
    }
}
