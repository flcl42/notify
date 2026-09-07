package dev.privatenotify;

import android.Manifest;
import android.app.Activity;
import android.app.AlertDialog;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.os.Build;
import android.os.Bundle;
import android.text.Editable;
import android.text.TextWatcher;
import android.text.TextUtils;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.view.WindowInsets;
import android.widget.*;
import com.google.zxing.integration.android.IntentIntegrator;
import com.google.zxing.integration.android.IntentResult;
import org.json.JSONArray;
import org.json.JSONObject;
import java.time.Instant;
import java.time.LocalDate;
import java.time.ZoneId;
import java.time.format.DateTimeFormatter;
import java.util.ArrayList;
import java.util.Locale;

public final class MainActivity extends Activity {
    static final String EXTRA_PAIRING_ADDED_NAME = "dev.privatenotify.extra.PAIRING_ADDED_NAME";
    private int INK, MUTED, GREEN, LINE;
    private NotifyStore store;
    private TextView subtitle, empty, sourceButton, levelButton;
    private CheckBox unreadButton;
    private LogAdapter adapter;
    private String query = "", source = "", level = "";
    private boolean unreadOnly;
    private final ArrayList<JSONObject> visible = new ArrayList<>();
    private final SharedPreferences.OnSharedPreferenceChangeListener listener = (prefs, key) -> {
        if ("notifications".equals(key) || "subscriptions".equals(key)) runOnUiThread(this::refresh);
    };

    @Override public void onCreate(Bundle state) {
        super.onCreate(state);
        INK = getColor(R.color.log_text); MUTED = getColor(R.color.log_muted);
        GREEN = getColor(R.color.log_accent); LINE = getColor(R.color.log_divider);
        store = new NotifyStore(this);
        if (state != null) {
            query = state.getString("query", ""); source = state.getString("source", "");
            level = state.getString("level", ""); unreadOnly = state.getBoolean("unread");
        }
        NotificationPresenter.ensureChannel(this);
        render();
        handleIntent(getIntent());
        if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED)
            requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, 11);
    }
    @Override protected void onResume() { super.onResume(); store.observe(listener); refresh(); }
    @Override protected void onPause() { store.stopObserving(listener); super.onPause(); }
    @Override protected void onSaveInstanceState(Bundle state) {
        super.onSaveInstanceState(state);
        state.putString("query", query); state.putString("source", source);
        state.putString("level", level); state.putBoolean("unread", unreadOnly);
    }
    @Override protected void onNewIntent(Intent intent) { super.onNewIntent(intent); setIntent(intent); handleIntent(intent); }
    private void handleIntent(Intent intent) {
        String added = intent.getStringExtra(EXTRA_PAIRING_ADDED_NAME);
        if (added != null) { intent.removeExtra(EXTRA_PAIRING_ADDED_NAME); toast("Source added: " + added); }
        String id = intent.getStringExtra("notificationId");
        if (id != null) {
            intent.removeExtra("notificationId");
            JSONArray records = store.notifications();
            for (int i = 0; i < records.length(); i++) {
                JSONObject record = records.optJSONObject(i);
                if (record != null && id.equals(record.optString("id"))) { showMessage(record); break; }
            }
        }
    }
    private void render() {
        if (Build.VERSION.SDK_INT >= 30) getWindow().setDecorFitsSystemWindows(false);
        LinearLayout root = column(); root.setBackgroundColor(getColor(R.color.log_background));
        LinearLayout header = row(); header.setPadding(dp(20), dp(12), dp(8), dp(8));
        LinearLayout titles = column(); titles.addView(text("nfy", 28, INK, true));
        subtitle = text("Notification log", 13, MUTED, false); titles.addView(subtitle);
        header.addView(titles, new LinearLayout.LayoutParams(0, -2, 1));
        header.addView(tool(R.drawable.ic_scan, "Scan source QR", this::startScanner));
        header.addView(tool(R.drawable.ic_sources, "Sources", this::showSources));
        ImageButton more = tool(R.drawable.ic_more, "Log actions", () -> {});
        more.setOnClickListener(v -> {
            PopupMenu menu = new PopupMenu(this, more);
            menu.getMenu().add("Mark all read").setOnMenuItemClickListener(item -> { store.markRead(null); return true; });
            menu.getMenu().add("Clear log").setOnMenuItemClickListener(item -> {
                new AlertDialog.Builder(this).setTitle("Clear notification log?")
                        .setMessage("All saved messages will be deleted. Your sources stay registered.")
                        .setNegativeButton("Cancel", null).setPositiveButton("Clear", (d, w) -> store.clearNotifications()).show();
                return true;
            }); menu.show();
        });
        header.addView(more); root.addView(header);
        EditText search = new EditText(this);
        search.setSingleLine(true); search.setTextSize(16); search.setTextColor(INK); search.setHintTextColor(MUTED);
        search.setHint("Search messages"); search.setPadding(dp(14), dp(10), dp(14), dp(10));
        search.setBackground(background(getColor(R.color.log_search), 8)); search.setText(query);
        search.setImeOptions(android.view.inputmethod.EditorInfo.IME_ACTION_SEARCH);
        search.setOnEditorActionListener((view, action, event) -> {
            if (action != android.view.inputmethod.EditorInfo.IME_ACTION_SEARCH) return false;
            getSystemService(android.view.inputmethod.InputMethodManager.class).hideSoftInputFromWindow(search.getWindowToken(), 0);
            search.clearFocus(); root.requestFocus(); return true;
        });
        LinearLayout.LayoutParams searchParams = new LinearLayout.LayoutParams(-1, -2);
        searchParams.setMargins(dp(20), dp(8), dp(20), dp(4)); root.addView(search, searchParams);
        search.addTextChangedListener(new TextWatcher() {
            public void beforeTextChanged(CharSequence s, int start, int count, int after) {}
            public void onTextChanged(CharSequence s, int start, int before, int count) { query = s.toString(); refresh(); }
            public void afterTextChanged(Editable e) {}
        });
        HorizontalScrollView filters = new HorizontalScrollView(this); filters.setHorizontalScrollBarEnabled(false);
        LinearLayout filterRow = row(); filterRow.setPadding(dp(12), 0, dp(12), 0);
        sourceButton = filter("All sources", this::chooseSource);
        levelButton = filter("All levels", this::chooseLevel);
        unreadButton = new CheckBox(this); unreadButton.setText("Unread");
        unreadButton.setTextSize(13); unreadButton.setTextColor(MUTED); unreadButton.setMinHeight(dp(48));
        unreadButton.setButtonTintList(android.content.res.ColorStateList.valueOf(GREEN));
        unreadButton.setOnCheckedChangeListener((button, checked) -> { unreadOnly = checked; refresh(); });
        filterRow.addView(sourceButton); filterRow.addView(levelButton); filterRow.addView(unreadButton);
        filters.addView(filterRow); root.addView(filters); root.addView(rule());
        FrameLayout content = new FrameLayout(this);
        ListView list = new ListView(this); list.setDivider(null); list.setCacheColorHint(0);
        list.setClipToPadding(false); list.setPadding(0, 0, 0, dp(16));
        adapter = new LogAdapter(); list.setAdapter(adapter);
        list.setOnItemClickListener((parent, view, position, id) -> showMessage(visible.get(position)));
        empty = text("No notifications yet", 17, MUTED, false); empty.setGravity(Gravity.CENTER);
        empty.setPadding(dp(24), dp(24), dp(24), dp(24));
        content.addView(list, new FrameLayout.LayoutParams(-1, -1));
        content.addView(empty, new FrameLayout.LayoutParams(-1, -1)); list.setEmptyView(empty);
        root.addView(content, new LinearLayout.LayoutParams(-1, 0, 1));
        root.setFocusableInTouchMode(true);
        root.setOnApplyWindowInsetsListener((view, insets) -> {
            if (Build.VERSION.SDK_INT >= 30) {
                android.graphics.Insets bars = insets.getInsets(WindowInsets.Type.systemBars() | WindowInsets.Type.ime());
                view.setPadding(bars.left, bars.top, bars.right, bars.bottom);
                header.setVisibility(insets.isVisible(WindowInsets.Type.ime()) ? View.GONE : View.VISIBLE);
            }
            return insets;
        });
        setContentView(root); root.requestApplyInsets(); refresh();
    }
    private void refresh() {
        if (adapter == null) return;
        JSONArray records = store.notifications(); visible.clear(); int unread = 0;
        for (int i = 0; i < records.length(); i++) {
            JSONObject record = records.optJSONObject(i); if (record == null) continue;
            if (!record.optBoolean("read")) unread++;
            if (unreadOnly && record.optBoolean("read")) continue;
            if (!source.isEmpty() && !source.equals(record.optString("subscriptionId"))) continue;
            if (!level.isEmpty() && !level.equals(MessageStyle.severity(record))) continue;
            String haystack = record.optString("title") + " " + record.optString("body") + " " + sourceName(record) + " " + record.optString("service");
            if (!haystack.toLowerCase(Locale.ROOT).contains(query.toLowerCase(Locale.ROOT))) continue;
            visible.add(record);
        }
        subtitle.setText(records.length() + " messages  \u00b7  " + unread + " unread");
        String sourceLabel = "All sources";
        if (!source.isEmpty()) {
            JSONObject sub = store.subscriptionById(source);
            sourceLabel = sub == null ? "Selected source" : sub.optString("name", "Source");
        }
        sourceButton.setText(sourceLabel + " \u2304");
        levelButton.setText((level.isEmpty() ? "All levels" : capitalize(level)) + " \u2304");
        unreadButton.setChecked(unreadOnly);
        unreadButton.setTextColor(unreadOnly ? GREEN : MUTED);
        empty.setText(records.length() == 0 ? "No notifications yet" : "No matching messages");
        adapter.notifyDataSetChanged();
    }
    private void chooseSource() {
        ArrayList<String> ids = new ArrayList<>(), names = new ArrayList<>(); ids.add(""); names.add("All sources");
        JSONArray subs = store.subscriptions();
        for (int i = 0; i < subs.length(); i++) {
            JSONObject sub = subs.optJSONObject(i); if (sub == null) continue;
            ids.add(sub.optString("id")); names.add(sub.optString("name", "Source"));
        }
        JSONArray records = store.notifications();
        for (int i = 0; i < records.length(); i++) {
            JSONObject record = records.optJSONObject(i);
            if (record != null && !ids.contains(record.optString("subscriptionId"))) {
                ids.add(record.optString("subscriptionId")); names.add(sourceName(record));
            }
        }
        new AlertDialog.Builder(this).setTitle("Source").setSingleChoiceItems(names.toArray(new String[0]), ids.indexOf(source),
                (dialog, which) -> { source = ids.get(which); dialog.dismiss(); refresh(); }).setNegativeButton("Cancel", null).show();
    }
    private void chooseLevel() {
        String[] labels = {"All levels", "Information", "Success", "Warning", "Error", "Emergency"};
        String[] values = {"", "info", "success", "warning", "error", "emergency"};
        int checked = java.util.Arrays.asList(values).indexOf(level);
        new AlertDialog.Builder(this).setTitle("Severity").setSingleChoiceItems(labels, checked,
                (dialog, which) -> { level = values[which]; dialog.dismiss(); refresh(); }).setNegativeButton("Cancel", null).show();
    }
    private void showSources() {
        JSONArray subs = store.subscriptions(); String[] names = new String[subs.length()];
        for (int i = 0; i < names.length; i++) names[i] = subs.optJSONObject(i).optString("name", "Source");
        AlertDialog.Builder builder = new AlertDialog.Builder(this).setTitle("Sources (" + names.length + ")")
                .setPositiveButton("Scan QR", (d, w) -> startScanner()).setNegativeButton("Done", null);
        if (names.length == 0) builder.setMessage("No registered sources");
        else builder.setItems(names, (dialog, which) -> {
            JSONObject sub = subs.optJSONObject(which);
            new AlertDialog.Builder(this).setTitle(names[which])
                    .setMessage(sub.optString("pushToken").isEmpty() ? "Registration pending" : "Registered")
                    .setNeutralButton("View messages", (d, w) -> { source = sub.optString("id"); refresh(); })
                    .setNegativeButton("Close", null).setPositiveButton("Remove source", (d, w) ->
                        new AlertDialog.Builder(this).setTitle("Remove " + names[which] + "?")
                                .setMessage("New messages from this source will no longer appear. Saved messages remain in the log.")
                                .setNegativeButton("Cancel", null).setPositiveButton("Remove", (confirm, button) -> {
                                    try { store.removeSubscription(sub.optString("id")); } catch (Exception e) { toast(e.getMessage()); }
                                }).show()).show();
        }); builder.show();
    }
    private void showMessage(JSONObject record) {
        store.markRead(record.optString("id"));
        ScrollView scroll = new ScrollView(this); LinearLayout body = column(); body.setPadding(dp(24), dp(12), dp(24), dp(24));
        body.addView(text(sourceName(record) + "  \u00b7  " + timestamp(record, "dd MMM yyyy, HH:mm:ss"), 13, MUTED, false));
        if (!MessageStyle.alert(record).equals("default"))
            body.addView(text("Requested alert: " + MessageStyle.alertLabel(MessageStyle.alert(record)), 13, MUTED, false));
        if (!MessageStyle.severity(record).isEmpty()) {
            TextView severity = text(MessageStyle.label(record), 14, MessageStyle.color(this, record), true);
            severity.setPadding(0, dp(12), 0, 0); body.addView(severity);
        }
        TextView title = text(MessageStyle.icon(record) + (MessageStyle.icon(record).isEmpty() ? "" : " ") + record.optString("title", "Notification"), 22, INK, true);
        title.setPadding(0, dp(14), 0, dp(12)); title.setTextIsSelectable(true); body.addView(title);
        TextView message = text(record.optString("body"), 17, INK, false); message.setTextIsSelectable(true); message.setLineSpacing(dp(4), 1); body.addView(message);
        scroll.addView(body);
        String plain = record.optString("title") + "\n\n" + record.optString("body");
        AlertDialog dialog = new AlertDialog.Builder(this).setView(scroll).setPositiveButton("Done", null)
                .setNeutralButton("Copy", (d, w) -> {
                    getSystemService(ClipboardManager.class).setPrimaryClip(ClipData.newPlainText("Notification", plain)); toast("Copied");
                }).setNegativeButton("Actions", null).create();
        dialog.setOnShowListener(d -> dialog.getButton(AlertDialog.BUTTON_NEGATIVE).setOnClickListener(v -> {
            PopupMenu menu = new PopupMenu(this, v);
            menu.getMenu().add("Share").setOnMenuItemClickListener(item -> {
                startActivity(Intent.createChooser(new Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, plain), "Share notification")); return true;
            });
            menu.getMenu().add("Delete message").setOnMenuItemClickListener(item -> {
                new AlertDialog.Builder(this).setTitle("Delete message?").setNegativeButton("Cancel", null)
                        .setPositiveButton("Delete", (confirm, button) -> { store.deleteNotification(record.optString("id")); dialog.dismiss(); }).show(); return true;
            }); menu.show();
        })); dialog.show();
    }
    private final class LogAdapter extends BaseAdapter {
        public int getCount() { return visible.size(); }
        public Object getItem(int position) { return visible.get(position); }
        public long getItemId(int position) { return position; }
        public View getView(int position, View recycled, ViewGroup parent) {
            Holder holder;
            if (recycled == null) { holder = new Holder(); recycled = holder.root; recycled.setTag(holder); }
            else holder = (Holder) recycled.getTag();
            JSONObject record = visible.get(position);
            String date = day(record);
            holder.date.setVisibility(position == 0 || !date.equals(day(visible.get(position - 1))) ? View.VISIBLE : View.GONE); holder.date.setText(date);
            holder.icon.setText(MessageStyle.glyph(record)); holder.icon.setTextColor(MessageStyle.color(MainActivity.this, record));
            holder.icon.setBackground(background(MessageStyle.tint(MainActivity.this, record), 8));
            holder.title.setText(record.optString("title", "Notification"));
            holder.title.setTypeface(Typeface.DEFAULT, record.optBoolean("read") ? Typeface.NORMAL : Typeface.BOLD);
            holder.body.setText(record.optString("body"));
            holder.meta.setText(sourceName(record) + "  \u00b7  " + timestamp(record, "HH:mm")
                    + (MessageStyle.severity(record).isEmpty() ? "" : "  \u00b7  " + MessageStyle.label(record)));
            holder.meta.setTextColor(MessageStyle.severity(record).isEmpty() ? MUTED : MessageStyle.color(MainActivity.this, record));
            holder.dot.setVisibility(record.optBoolean("read") ? View.INVISIBLE : View.VISIBLE);
            return recycled;
        }
    }
    private final class Holder {
        final LinearLayout root = column();
        final TextView date = text("", 12, MUTED, true), icon = text("", 24, GREEN, false);
        final TextView title = text("", 16, INK, true), body = text("", 14, MUTED, false), meta = text("", 12, MUTED, false);
        final View dot = new View(MainActivity.this);
        Holder() {
            date.setPadding(dp(20), dp(18), dp(20), dp(8)); root.addView(date);
            LinearLayout message = row(); message.setGravity(Gravity.TOP); message.setPadding(dp(20), dp(14), dp(20), dp(14));
            icon.setGravity(Gravity.CENTER); icon.setSingleLine(true); icon.setAutoSizeTextTypeUniformWithConfiguration(8, 24, 1, android.util.TypedValue.COMPLEX_UNIT_SP);
            message.addView(icon, new LinearLayout.LayoutParams(dp(44), dp(44)));
            LinearLayout content = column(); content.setPadding(dp(12), 0, dp(8), 0);
            title.setMaxLines(2); title.setEllipsize(TextUtils.TruncateAt.END); content.addView(title);
            body.setMaxLines(3); body.setEllipsize(TextUtils.TruncateAt.END); body.setPadding(0, dp(4), 0, dp(8)); content.addView(body);
            meta.setMaxLines(2); meta.setEllipsize(TextUtils.TruncateAt.END); content.addView(meta);
            message.addView(content, new LinearLayout.LayoutParams(0, -2, 1));
            dot.setBackground(background(GREEN, 4)); LinearLayout.LayoutParams dotParams = new LinearLayout.LayoutParams(dp(6), dp(6)); dotParams.topMargin = dp(7);
            message.addView(dot, dotParams); root.addView(message); root.addView(rule());
        }
    }
    private String sourceName(JSONObject record) { return record.optString("subscriptionName", record.optString("service", "Source")); }
    private Instant received(JSONObject record) {
        try { return Instant.parse(record.optString("receivedAt", record.optString("createdAt"))); } catch (Exception e) { return Instant.EPOCH; }
    }
    private String timestamp(JSONObject record, String pattern) {
        if (received(record).equals(Instant.EPOCH)) return "Unknown time";
        return DateTimeFormatter.ofPattern(pattern, Locale.getDefault()).withZone(ZoneId.systemDefault()).format(received(record));
    }
    private String day(JSONObject record) {
        LocalDate date = received(record).atZone(ZoneId.systemDefault()).toLocalDate();
        if (date.equals(LocalDate.now())) return "TODAY";
        if (date.equals(LocalDate.now().minusDays(1))) return "YESTERDAY";
        return timestamp(record, "dd MMM yyyy").toUpperCase(Locale.getDefault());
    }
    private String capitalize(String value) { return value.substring(0, 1).toUpperCase(Locale.ROOT) + value.substring(1); }
    private LinearLayout column() { LinearLayout view = new LinearLayout(this); view.setOrientation(LinearLayout.VERTICAL); return view; }
    private LinearLayout row() { LinearLayout view = new LinearLayout(this); view.setGravity(Gravity.CENTER_VERTICAL); return view; }
    private TextView text(String value, int size, int color, boolean bold) {
        TextView view = new TextView(this); view.setText(value); view.setTextSize(size); view.setTextColor(color);
        view.setTypeface(Typeface.create("sans-serif", bold ? Typeface.BOLD : Typeface.NORMAL)); view.setLetterSpacing(0); return view;
    }
    private View rule() { View view = new View(this); view.setBackgroundColor(LINE); view.setLayoutParams(new LinearLayout.LayoutParams(-1, dp(1))); return view; }
    private GradientDrawable background(int color, int radius) { GradientDrawable drawable = new GradientDrawable(); drawable.setColor(color); drawable.setCornerRadius(dp(radius)); return drawable; }
    private TextView filter(String label, Runnable action) {
        TextView view = text(label, 13, MUTED, true); view.setGravity(Gravity.CENTER);
        view.setPadding(dp(10), dp(14), dp(10), dp(14)); view.setMinHeight(dp(48)); view.setMaxWidth(dp(200));
        view.setMaxLines(1); view.setEllipsize(TextUtils.TruncateAt.END); view.setOnClickListener(v -> action.run()); return view;
    }
    private ImageButton tool(int icon, String label, Runnable action) {
        ImageButton view = new ImageButton(this); view.setImageResource(icon); view.setColorFilter(INK);
        android.util.TypedValue selectable = new android.util.TypedValue();
        getTheme().resolveAttribute(android.R.attr.selectableItemBackgroundBorderless, selectable, true);
        view.setBackgroundResource(selectable.resourceId); view.setContentDescription(label); view.setTooltipText(label);
        view.setLayoutParams(new LinearLayout.LayoutParams(dp(48), dp(48))); view.setOnClickListener(v -> action.run()); return view;
    }
    private void startScanner() {
        if (checkSelfPermission(Manifest.permission.CAMERA) != PackageManager.PERMISSION_GRANTED) { requestPermissions(new String[]{Manifest.permission.CAMERA}, 10); return; }
        IntentIntegrator scanner = new IntentIntegrator(this); scanner.setDesiredBarcodeFormats(IntentIntegrator.QR_CODE);
        scanner.setPrompt(""); scanner.setBeepEnabled(false); scanner.setOrientationLocked(false); scanner.initiateScan();
    }
    @Override public void onRequestPermissionsResult(int code, String[] permissions, int[] results) {
        super.onRequestPermissionsResult(code, permissions, results);
        if (code == 10 && results.length > 0 && results[0] == PackageManager.PERMISSION_GRANTED) startScanner();
    }
    @Override protected void onActivityResult(int code, int resultCode, Intent data) {
        IntentResult result = IntentIntegrator.parseActivityResult(code, resultCode, data);
        if (result == null) { super.onActivityResult(code, resultCode, data); return; }
        if (result.getContents() == null) return;
        try {
            PairingRegistrar.register(this, Protocol.parsePairingCode(result.getContents()), new PairingRegistrar.Callback() {
                public void onSuccess(JSONObject subscription) { runOnUiThread(() -> { toast("Source added: " + subscription.optString("name")); refresh(); }); }
                public void onError(Exception error) { runOnUiThread(() -> toast("Registration failed: " + error.getMessage())); }
            });
        } catch (Exception e) { toast("Pairing failed: " + e.getMessage()); }
    }
    private void toast(String value) { Toast.makeText(this, value, Toast.LENGTH_LONG).show(); }
    private int dp(int value) { return Math.round(value * getResources().getDisplayMetrics().density); }
}
