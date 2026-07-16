package dev.privatenotify;

import android.Manifest;
import android.app.Activity;
import android.content.pm.PackageManager;
import android.graphics.Typeface;
import android.os.Build;
import android.os.Bundle;
import android.view.Gravity;
import android.view.View;
import android.widget.Button;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;

import com.google.zxing.integration.android.IntentIntegrator;
import com.google.zxing.integration.android.IntentResult;

import org.json.JSONArray;
import org.json.JSONObject;

public final class MainActivity extends Activity {
    private NotifyStore store;
    private LinearLayout subscriptionsList;
    private LinearLayout inboxList;
    private TextView status;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        store = new NotifyStore(this);
        NotificationPresenter.ensureChannel(this);
        requestNotificationPermission();
        render();
    }

    @Override
    protected void onResume() {
        super.onResume();
        refreshLists();
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, android.content.Intent data) {
        IntentResult result = IntentIntegrator.parseActivityResult(requestCode, resultCode, data);
        if (result != null) {
            if (result.getContents() != null) {
                handlePairingCode(result.getContents());
            }
            return;
        }
        super.onActivityResult(requestCode, resultCode, data);
    }

    private void render() {
        ScrollView scroll = new ScrollView(this);
        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setPadding(dp(18), dp(18), dp(18), dp(28));
        root.setBackgroundColor(0xFFF7F8F8);
        scroll.addView(root);

        TextView title = text("Private Notify", 28, 0xFF162321, Typeface.BOLD);
        root.addView(title);

        status = text("", 14, 0xFF62716E, Typeface.NORMAL);
        status.setPadding(0, dp(4), 0, dp(14));
        root.addView(status);

        Button scan = button("Scan QR");
        scan.setOnClickListener(view -> startScanner());
        root.addView(scan);

        if (!BuildConfig.FIREBASE_CONFIGURED) {
            TextView warning = text(
                    "Firebase is not configured. Add android/app/google-services.json to enable FCM token registration.",
                    14,
                    0xFF8A5A00,
                    Typeface.BOLD
            );
            warning.setPadding(0, dp(12), 0, dp(8));
            root.addView(warning);
        }

        root.addView(sectionTitle("Subscriptions"));
        subscriptionsList = new LinearLayout(this);
        subscriptionsList.setOrientation(LinearLayout.VERTICAL);
        root.addView(subscriptionsList);

        LinearLayout inboxHeader = new LinearLayout(this);
        inboxHeader.setOrientation(LinearLayout.HORIZONTAL);
        inboxHeader.setGravity(Gravity.CENTER_VERTICAL);
        inboxHeader.setPadding(0, dp(18), 0, dp(8));
        TextView inboxTitle = sectionTitle("Inbox");
        inboxHeader.addView(inboxTitle, new LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1));
        Button clear = button("Clear");
        clear.setOnClickListener(view -> {
            store.clearNotifications();
            refreshLists();
        });
        inboxHeader.addView(clear);
        root.addView(inboxHeader);

        inboxList = new LinearLayout(this);
        inboxList.setOrientation(LinearLayout.VERTICAL);
        root.addView(inboxList);

        setContentView(scroll);
        refreshLists();
    }

    private void startScanner() {
        if (checkSelfPermission(Manifest.permission.CAMERA) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(new String[]{Manifest.permission.CAMERA}, 10);
            return;
        }

        IntentIntegrator integrator = new IntentIntegrator(this);
        integrator.setDesiredBarcodeFormats(IntentIntegrator.QR_CODE);
        integrator.setPrompt("");
        integrator.setBeepEnabled(false);
        integrator.setOrientationLocked(false);
        integrator.initiateScan();
    }

    private void handlePairingCode(String contents) {
        try {
            JSONObject pairing = Protocol.parsePairingCode(contents);
            registerPushToken(pairing);
        } catch (Exception error) {
            toast("Pairing failed: " + error.getMessage());
        }
    }

    private void registerPushToken(JSONObject pairing) throws Exception {
        PairingRegistrar.register(this, pairing, new PairingRegistrar.Callback() {
            @Override
            public void onSuccess(JSONObject subscription) {
                runOnUiThread(() -> {
                    toast("Push registration complete.");
                    refreshLists();
                });
            }

            @Override
            public void onError(Exception error) {
                runOnUiThread(() -> toast("Registration failed: " + error.getMessage()));
            }
        });
    }

    private void refreshLists() {
        if (subscriptionsList == null || inboxList == null || status == null) {
            return;
        }

        JSONArray subscriptions = store.subscriptions();
        JSONArray notifications = store.notifications();
        status.setText(subscriptions.length() + " subscription(s) · " + notifications.length() + " notification(s)");
        subscriptionsList.removeAllViews();
        inboxList.removeAllViews();

        if (subscriptions.length() == 0) {
            subscriptionsList.addView(card("No subscriptions", "Scan a private CLI pairing QR code.", null));
        } else {
            for (int index = 0; index < subscriptions.length(); index += 1) {
                JSONObject item = subscriptions.optJSONObject(index);
                if (item != null) {
                    subscriptionsList.addView(card(
                            item.optString("name", "phone"),
                            item.optString("defaultTitle", "Notification") + " · fcm · "
                                    + (item.optString("pushToken").isEmpty() ? "token missing" : "push-ready"),
                            () -> {
                                try {
                                    store.removeSubscription(item.optString("id"));
                                    refreshLists();
                                } catch (Exception error) {
                                    toast(error.getMessage());
                                }
                            }
                    ));
                }
            }
        }

        if (notifications.length() == 0) {
            inboxList.addView(card("No notifications", "", null));
        } else {
            for (int index = 0; index < notifications.length(); index += 1) {
                JSONObject item = notifications.optJSONObject(index);
                if (item != null) {
                    inboxList.addView(card(
                            item.optString("title", "Notification"),
                            item.optString("service", "service") + " · " + item.optString("body", ""),
                            null
                    ));
                }
            }
        }
    }

    private TextView sectionTitle(String value) {
        TextView view = text(value, 18, 0xFF162321, Typeface.BOLD);
        view.setPadding(0, dp(18), 0, dp(8));
        return view;
    }

    private View card(String title, String detail, Runnable deleteAction) {
        LinearLayout card = new LinearLayout(this);
        card.setOrientation(LinearLayout.VERTICAL);
        card.setPadding(dp(14), dp(12), dp(14), dp(12));
        card.setBackgroundColor(0xFFFFFFFF);
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT,
                LinearLayout.LayoutParams.WRAP_CONTENT
        );
        params.setMargins(0, 0, 0, dp(8));
        card.setLayoutParams(params);

        card.addView(text(title, 16, 0xFF162321, Typeface.BOLD));
        if (!detail.isEmpty()) {
            TextView detailView = text(detail, 14, 0xFF394A47, Typeface.NORMAL);
            detailView.setPadding(0, dp(4), 0, 0);
            card.addView(detailView);
        }

        if (deleteAction != null) {
            Button delete = button("Delete");
            delete.setOnClickListener(view -> deleteAction.run());
            card.addView(delete);
        }
        return card;
    }

    private TextView text(String value, int sp, int color, int style) {
        TextView view = new TextView(this);
        view.setText(value);
        view.setTextSize(sp);
        view.setTextColor(color);
        view.setTypeface(Typeface.DEFAULT, style);
        return view;
    }

    private Button button(String value) {
        Button button = new Button(this);
        button.setText(value);
        button.setAllCaps(false);
        return button;
    }

    private void requestNotificationPermission() {
        if (Build.VERSION.SDK_INT >= 33
                && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, 11);
        }
    }

    private void toast(String message) {
        Toast.makeText(this, message, Toast.LENGTH_LONG).show();
    }

    private int dp(int value) {
        return Math.round(value * getResources().getDisplayMetrics().density);
    }
}
