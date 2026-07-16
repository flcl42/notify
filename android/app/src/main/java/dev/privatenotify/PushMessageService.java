package dev.privatenotify;

import com.google.firebase.messaging.FirebaseMessagingService;
import com.google.firebase.messaging.RemoteMessage;

import org.json.JSONObject;

public final class PushMessageService extends FirebaseMessagingService {
    @Override
    public void onNewToken(String token) {
        new NotifyStore(this).setFcmToken(token);
    }

    @Override
    public void onMessageReceived(RemoteMessage message) {
        String envelope = message.getData().get("encryptedEnvelope");
        String subscriptionId = message.getData().get("subscriptionId");
        if (envelope == null || subscriptionId == null) {
            return;
        }

        try {
            NotifyStore store = new NotifyStore(this);
            JSONObject subscription = store.subscriptionById(subscriptionId);
            if (subscription == null) {
                return;
            }

            JSONObject decrypted = Protocol.decryptNotification(subscription, envelope);
            JSONObject record = store.addNotification(subscription, decrypted);
            NotificationPresenter.show(this, record);
        } catch (Exception ignored) {
            // A malformed or unauthenticated payload should not keep the service alive.
        }
    }
}
