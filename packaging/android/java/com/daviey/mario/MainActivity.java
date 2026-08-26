package com.daviey.mario;

import android.annotation.SuppressLint;
import android.app.Activity;
import android.os.Bundle;
import android.util.Log;
import android.view.View;
import android.view.WindowManager;
import android.webkit.ConsoleMessage;
import android.webkit.WebChromeClient;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.HashMap;

/**
 * Fullscreen WebView hosting the WASM build bundled in the APK assets.
 *
 * The bundle is served from a virtual https origin (request
 * interception): fetch() of mario.wasm fails on file:// origins, and a
 * real origin keeps the page's relative paths, Supabase CORS and
 * AudioContext gesture unlock all working unchanged. The on-screen
 * gamepad in index.html provides input; there is no keyboard path.
 */
public class MainActivity extends Activity {

    /** Virtual origin; every path under it maps to an APK asset. */
    private static final String ASSETS = "https://appassets.androidplatform.net/assets/";

    private WebView web;

    @SuppressLint("SetJavaScriptEnabled")
    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        // Arcade session: never let the screen sleep mid-run.
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);

        web = new WebView(this);
        // chrome://inspect, debuggable builds only (release ships clean).
        if ((getApplicationInfo().flags & android.content.pm.ApplicationInfo.FLAG_DEBUGGABLE) != 0) {
            WebView.setWebContentsDebuggingEnabled(true);
        }
        WebSettings s = web.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true); // local best + daily pad state
        s.setAllowFileAccess(false);
        s.setAllowContentAccess(false);

        web.setWebViewClient(new WebViewClient() {
            @Override
            public WebResourceResponse shouldInterceptRequest(WebView view, WebResourceRequest req) {
                return asset(req.getUrl().toString());
            }
        });
        web.setWebChromeClient(new WebChromeClient() {
            @Override
            public boolean onConsoleMessage(ConsoleMessage m) {
                Log.d("mario", m.message() + " @" + m.sourceId() + ":" + m.lineNumber());
                return true;
            }
        });
        // Touch-only: swallow long-press so nothing pops text menus.
        web.setOnLongClickListener(new View.OnLongClickListener() {
            @Override
            public boolean onLongClick(View v) {
                return true;
            }
        });
        web.setHapticFeedbackEnabled(false);

        setContentView(web);
        // ?touch: the shell is touch-only by construction — never let
        // pointer media queries (wrong on some devices) hide the pad.
        web.loadUrl(ASSETS + "index.html?touch");
    }

    /** Serve one APK asset for a virtual-origin request; null = network. */
    private WebResourceResponse asset(String url) {
        if (!url.startsWith(ASSETS)) {
            return null; // Supabase leaderboard calls go straight out
        }
        String path = url.substring(ASSETS.length());
        int q = path.indexOf('?');
        if (q >= 0) {
            path = path.substring(0, q);
        }
        if (path.isEmpty()) {
            path = "index.html";
        }
        // The page registers a CACHE-FIRST service worker on http(s)
        // origins — ours qualifies. In the APK that is pure poison: the
        // assets ship inside the app, and a stale SW keeps serving the
        // previous version's boot.js/wasm after an update. Serve a no-op
        // SW that claims immediately and purges any old caches instead.
        if (path.equals("sw.js")) {
            String noop = "self.addEventListener('install',e=>self.skipWaiting());"
                    + "self.addEventListener('activate',e=>{e.waitUntil(caches.keys()"
                    + ".then(ks=>Promise.all(ks.map(k=>caches.delete(k)))).then(()=>self.clients.claim()))});"
                    + "// no fetch handler: every request goes to the APK assets";
            return new WebResourceResponse("text/javascript", "utf-8",
                    new ByteArrayInputStream(noop.getBytes(java.nio.charset.StandardCharsets.UTF_8)));
        }
        try {
            InputStream in = getAssets().open(path);
            // No charset: mixed text/binary assets.
            return new WebResourceResponse(mime(path), null, in);
        } catch (IOException e) {
            return notFound();
        }
    }

    private static String mime(String path) {
        if (path.endsWith(".wasm")) return "application/wasm";
        if (path.endsWith(".js")) return "text/javascript";
        if (path.endsWith(".html")) return "text/html";
        if (path.endsWith(".json") || path.endsWith(".webmanifest")) return "application/json";
        if (path.endsWith(".png")) return "image/png";
        if (path.endsWith(".css")) return "text/css";
        if (path.endsWith(".svg")) return "image/svg+xml";
        return "application/octet-stream";
    }

    private static WebResourceResponse notFound() {
        return new WebResourceResponse("text/plain", "utf-8", 404, "Not Found",
                new HashMap<String, String>(), new ByteArrayInputStream(new byte[0]));
    }

    /** Immersive sticky: game owns the whole screen. Legacy flags are
     *  no-ops on Android 15+ with targetSdk 35, so use InsetsController
     *  there and keep the flags for older devices. */
    private void hideBars() {
        if (android.os.Build.VERSION.SDK_INT >= 30) {
            android.view.WindowInsetsController c = getWindow().getInsetsController();
            if (c != null) {
                c.hide(android.view.WindowInsets.Type.systemBars());
                c.setSystemBarsBehavior(
                        android.view.WindowInsetsController.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE);
            }
            getWindow().setDecorFitsSystemWindows(false);
        } else {
            web.setSystemUiVisibility(View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
                    | View.SYSTEM_UI_FLAG_FULLSCREEN
                    | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                    | View.SYSTEM_UI_FLAG_LAYOUT_STABLE
                    | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                    | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION);
        }
    }

    @Override
    public void onWindowFocusChanged(boolean hasFocus) {
        super.onWindowFocusChanged(hasFocus);
        if (hasFocus) {
            hideBars();
        }
    }

    /** Background instead of die: the WebView (and the run) survives. */
    @Override
    public void onBackPressed() {
        moveTaskToBack(true);
    }

    @Override
    protected void onPause() {
        web.onPause(); // stop rAF/JS timers while backgrounded
        super.onPause();
    }

    @Override
    protected void onResume() {
        super.onResume();
        web.onResume();
    }

    @Override
    protected void onDestroy() {
        web.destroy();
        super.onDestroy();
    }
}
