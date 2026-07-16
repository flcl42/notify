package dev.privatenotify;

import android.net.Uri;
import android.util.Base64;

import org.json.JSONObject;

import java.nio.charset.StandardCharsets;

import javax.crypto.Cipher;
import javax.crypto.spec.IvParameterSpec;
import javax.crypto.spec.SecretKeySpec;

final class Protocol {
    static final String PAIRING_SCHEME = "dev.privatenotify";
    private static final String LEGACY_PAIRING_SCHEME = "notify";

    private Protocol() {
    }

    static JSONObject parsePairingCode(String raw) throws Exception {
        Uri uri = Uri.parse(raw.trim());
        if (!isPairingScheme(uri.getScheme()) || !"pair".equals(uri.getHost())) {
            throw new IllegalArgumentException("QR code is not a Private Notify pairing code.");
        }

        String payload = uri.getQueryParameter("payload");
        if (payload == null || payload.isEmpty()) {
            throw new IllegalArgumentException("Pairing code has no payload.");
        }

        JSONObject json = new JSONObject(new String(decodeBase64Url(payload), StandardCharsets.UTF_8));
        if (json.optInt("v") != 1 || !"notify-pairing".equals(json.optString("type"))) {
            throw new IllegalArgumentException("Pairing payload version is not supported.");
        }
        if (!"push".equals(json.optString("delivery"))) {
            throw new IllegalArgumentException("Native Android uses push pairing. Create the QR with `notify pair`.");
        }
        if (json.optString("registrationUrl").isEmpty()) {
            throw new IllegalArgumentException("Pairing payload is missing registration URL.");
        }
        if (decodeBase64Url(json.optString("key")).length != 32) {
            throw new IllegalArgumentException("Pairing key must be 32 bytes.");
        }
        return json;
    }

    private static boolean isPairingScheme(String scheme) {
        return PAIRING_SCHEME.equals(scheme) || LEGACY_PAIRING_SCHEME.equals(scheme);
    }

    static JSONObject decryptNotification(JSONObject subscription, String envelopeJson) throws Exception {
        JSONObject envelope = new JSONObject(envelopeJson);
        if (!"notification".equals(envelope.optString("type")) || envelope.optInt("v") != 1) {
            throw new IllegalArgumentException("Unsupported notification envelope.");
        }
        if (!subscription.optString("id").equals(envelope.optString("subscriptionId"))) {
            throw new IllegalArgumentException("Notification subscription does not match.");
        }

        byte[] key = decodeBase64Url(subscription.getString("key"));
        byte[] nonce = decodeBase64Url(envelope.getString("nonce"));
        byte[] ciphertext = decodeBase64Url(envelope.getString("ciphertext"));

        Cipher cipher = Cipher.getInstance("ChaCha20-Poly1305");
        cipher.init(Cipher.DECRYPT_MODE, new SecretKeySpec(key, "ChaCha20"), new IvParameterSpec(nonce));

        return new JSONObject(new String(cipher.doFinal(ciphertext), StandardCharsets.UTF_8));
    }

    static byte[] decodeBase64Url(String value) {
        String padded = value;
        int remainder = value.length() % 4;
        if (remainder > 0) {
            padded = value + "====".substring(remainder);
        }
        return Base64.decode(padded, Base64.URL_SAFE | Base64.NO_WRAP);
    }
}
