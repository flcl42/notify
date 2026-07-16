package dev.privatenotify;

import android.app.Activity;
import android.content.Intent;
import android.os.Bundle;
import android.util.Log;

import org.json.JSONObject;

public final class PairingActivity extends Activity {
    private static final String TAG = "PrivateNotifyPair";
    private int registrationAttempt;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        register(getIntent());
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        register(intent);
    }

    private void register(Intent intent) {
        int attempt = ++registrationAttempt;
        NotificationPresenter.ensureChannel(this);
        String contents = intent == null || intent.getData() == null
                ? ""
                : intent.getData().toString();

        try {
            JSONObject pairing = Protocol.parsePairingCode(contents);
            PairingRegistrar.register(this, pairing, new PairingRegistrar.Callback() {
                @Override
                public void onSuccess(JSONObject subscription) {
                    Log.i(TAG, "Push registration complete for " + subscription.optString("id"));
                    finishRegistration(attempt);
                }

                @Override
                public void onError(Exception error) {
                    Log.w(TAG, "Registration failed", error);
                    finishRegistration(attempt);
                }
            });
        } catch (Exception error) {
            Log.w(TAG, "Pairing failed", error);
            finishRegistration(attempt);
        }
    }

    private void finishRegistration(int attempt) {
        runOnUiThread(() -> {
            if (attempt != registrationAttempt || isFinishing()) {
                return;
            }
            finishAndRemoveTask();
            overridePendingTransition(0, 0);
        });
    }
}
