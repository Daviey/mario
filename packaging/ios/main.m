// main.m — the iOS twin of packaging/android's MainActivity: a thin
// WKWebView shell around the WASM web build (dist/web bundled as www/).
//
// The page fetches ./mario.wasm, and WebKit gives file:// URLs neither
// fetch nor sane origin isolation — so instead of loadFileURL we register
// a custom URL scheme (marioapp://) and serve the bundled files through a
// WKURLSchemeHandler, exactly the role Android's asset interception
// plays. Everything (wasm fetch, canvas, touch pad, WebAudio, supabase
// leaderboard over https) then behaves as on the real site.
//
// Built entirely on Linux by tools/mkipa: clang -target arm64-apple-ios,
// linked with lld against the iPhoneOS SDK stubs, ad-hoc signed with
// ldid. Landscape-only, status bar hidden (the game has its own HUD).
#import <Foundation/Foundation.h>
#import <UIKit/UIKit.h>
#import <WebKit/WebKit.h>

static NSString *const kScheme = @"marioapp";
static NSString *const kStartURL = @"marioapp://localhost/index.html?touch";

static NSString *mimeType(NSString *path) {
    NSString *ext = [[path pathExtension] lowercaseString];
    if ([ext isEqualToString:@"html"] || [ext isEqualToString:@"htm"]) return @"text/html; charset=utf-8";
    if ([ext isEqualToString:@"js"]) return @"text/javascript; charset=utf-8";
    if ([ext isEqualToString:@"wasm"]) return @"application/wasm";
    if ([ext isEqualToString:@"json"] || [ext isEqualToString:@"webmanifest"])
        return @"application/manifest+json; charset=utf-8";
    if ([ext isEqualToString:@"png"]) return @"image/png";
    if ([ext isEqualToString:@"svg"]) return @"image/svg+xml";
    if ([ext isEqualToString:@"ico"]) return @"image/x-icon";
    if ([ext isEqualToString:@"css"]) return @"text/css; charset=utf-8";
    if ([ext isEqualToString:@"txt"]) return @"text/plain; charset=utf-8";
    return @"application/octet-stream";
}

// Serves <bundle>/www/** synchronously. Files are small and local; the
// only multi-MB one (mario.wasm) is read once at startup.
@interface MarioSchemeHandler : NSObject <WKURLSchemeHandler>
@end

@implementation MarioSchemeHandler

- (void)webView:(WKWebView *)webView startURLSchemeTask:(id<WKURLSchemeTask>)task {
    NSString *path = [[task.request.URL.path stringByRemovingPercentEncoding]
        stringByReplacingOccurrencesOfString:@"+" withString:@" "];
    if (path.length == 0 || [path isEqualToString:@"/"]) path = @"/index.html";

    NSString *root = [[[NSBundle mainBundle] resourcePath] stringByAppendingPathComponent:@"www"];
    NSString *file = [root stringByAppendingPathComponent:path];
    // Reject traversal before touching the filesystem.
    NSString *canon = [file stringByStandardizingPath];
    if (![canon hasPrefix:[root stringByAppendingString:@"/"]]) {
        [self respond:task status:404 type:@"text/plain" data:[@"not found" dataUsingEncoding:NSUTF8StringEncoding]];
        return;
    }

    NSData *data = [NSData dataWithContentsOfFile:canon];
    if (data == nil) {
        [self respond:task status:404 type:@"text/plain" data:[@"not found" dataUsingEncoding:NSUTF8StringEncoding]];
        return;
    }
    [self respond:task status:200 type:mimeType(canon) data:data];
}

- (void)respond:(id<WKURLSchemeTask>)task status:(NSInteger)status type:(NSString *)type data:(NSData *)data {
    NSDictionary *headers = @{@"Content-Type": type, @"Content-Length": [NSString stringWithFormat:@"%lu", (unsigned long)data.length]};
    NSHTTPURLResponse *resp =
        [[NSHTTPURLResponse alloc] initWithURL:task.request.URL
                                     statusCode:status
                                    HTTPVersion:@"HTTP/1.1"
                                   headerFields:headers];
    [task didReceiveResponse:resp];
    [task didReceiveData:data];
    [task didFinish];
}

- (void)webView:(WKWebView *)webView stopURLSchemeTask:(id<WKURLSchemeTask>)task {
    // Serving is synchronous; nothing to cancel.
}

@end

@interface AppDelegate : UIResponder <UIApplicationDelegate>
@property (strong, nonatomic) UIWindow *window;
@end

@implementation AppDelegate

- (BOOL)application:(UIApplication *)application
    didFinishLaunchingWithOptions:(NSDictionary *)launchOptions {
    self.window = [[UIWindow alloc] initWithFrame:[UIScreen mainScreen].bounds];
    self.window.backgroundColor = [UIColor blackColor];

    WKWebViewConfiguration *cfg = [[WKWebViewConfiguration alloc] init];
    [cfg setURLSchemeHandler:[[MarioSchemeHandler alloc] init] forURLScheme:kScheme];

    WKWebView *wv = [[WKWebView alloc] initWithFrame:self.window.bounds configuration:cfg];
    wv.autoresizingMask = UIViewAutoresizingFlexibleWidth | UIViewAutoresizingFlexibleHeight;
    wv.allowsBackForwardNavigationGestures = NO;
    wv.allowsLinkPreview = NO;
    // The page handles its own safe-area padding via env() (viewport-fit
    // cover); stop WebKit from double-insetting the scroll view.
    wv.scrollView.contentInsetAdjustmentBehavior = UIScrollViewContentInsetAdjustmentNever;
    wv.scrollView.bounces = NO;
    [wv loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:kStartURL]]];

    UIViewController *vc = [[UIViewController alloc] init];
    vc.view = wv;
    self.window.rootViewController = vc;
    [self.window makeKeyAndVisible];
    return YES;
}

@end

int main(int argc, char *argv[]) {
    @autoreleasepool {
        return UIApplicationMain(argc, argv, nil, NSStringFromClass([AppDelegate class]));
    }
}
