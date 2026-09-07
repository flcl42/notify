package dev.privatenotify;

import android.content.Context;
import android.graphics.Bitmap;
import android.graphics.Canvas;
import android.graphics.Paint;
import android.graphics.Typeface;
import org.json.JSONObject;
import java.util.Locale;

final class MessageStyle {
    static String alert(JSONObject record) {
        String value = record.optString("alert", "").toLowerCase(Locale.ROOT);
        switch (value) {
            case "sound": case "vibrate": case "sound-vibrate": case "silent": return value;
            default: return "default";
        }
    }

    static String alertLabel(String alert) {
        switch (alert) {
            case "sound": return "Sound";
            case "vibrate": return "Vibration only";
            case "sound-vibrate": return "Sound and vibration";
            case "silent": return "Silent";
            default: return "Default";
        }
    }

    static String severity(JSONObject record) {
        String value = record.optString("severity", "").toLowerCase(Locale.ROOT);
        switch (value) {
            case "info": case "success": case "warning": case "error": case "emergency": return value;
            default: return "";
        }
    }

    static String icon(JSONObject record) {
        String value = record.optString("icon", "").trim();
        if (value.codePointCount(0, value.length()) > 16) return "";
        for (int i = 0; i < value.length();) {
            int code = value.codePointAt(i);
            if (Character.isISOControl(code) || code == 0x2028 || code == 0x2029) return "";
            i += Character.charCount(code);
        }
        return value;
    }

    static int color(Context context, JSONObject record) {
        int resource;
        switch (severity(record)) {
            case "info": resource = R.color.severity_info; break;
            case "success": resource = R.color.severity_success; break;
            case "warning": resource = R.color.severity_warning; break;
            case "error": resource = R.color.severity_error; break;
            case "emergency": resource = R.color.severity_emergency; break;
            default: resource = R.color.severity_neutral;
        }
        return context.getColor(resource);
    }

    static int tint(Context context, JSONObject record) {
        return (color(context, record) & 0x00FFFFFF) | 0x16000000;
    }

    static String label(JSONObject record) {
        String value = severity(record);
        return value.isEmpty() ? "" : value.substring(0, 1).toUpperCase(Locale.ROOT) + value.substring(1);
    }

    static String glyph(JSONObject record) {
        String supplied = icon(record);
        if (!supplied.isEmpty()) return supplied;
        switch (severity(record)) {
            case "info": return "i";
            case "success": return "\u2713";
            case "warning": case "error": case "emergency": return "!";
            default:
                String source = record.optString("subscriptionName", record.optString("title", "N")).trim();
                return source.isEmpty() ? "N" : source.substring(0, source.offsetByCodePoints(0, 1)).toUpperCase(Locale.ROOT);
        }
    }

    static Bitmap largeIcon(Context context, JSONObject record) {
        Bitmap bitmap = Bitmap.createBitmap(128, 128, Bitmap.Config.ARGB_8888);
        Canvas canvas = new Canvas(bitmap);
        Paint paint = new Paint(Paint.ANTI_ALIAS_FLAG);
        paint.setColor(tint(context, record));
        canvas.drawRoundRect(0, 0, 128, 128, 24, 24, paint);
        paint.setColor(color(context, record));
        paint.setTypeface(Typeface.create("sans-serif", Typeface.NORMAL));
        paint.setTextSize(76);
        String glyph = glyph(record);
        float width = paint.measureText(glyph);
        if (width > 104) paint.setTextSize(76 * 104 / width);
        paint.setTextAlign(Paint.Align.CENTER);
        canvas.drawText(glyph, 64, 64 - (paint.ascent() + paint.descent()) / 2, paint);
        return bitmap;
    }
}
