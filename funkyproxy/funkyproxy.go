package funkyproxy

import (
    "fmt"
    "image"
    "image/color"
    "image/draw"
    "image/gif"
    "image/jpeg"
    "image/png"
    "io"
    "net/http"
    "net/url"
    "os"
    "strings"
    "time"

    "appengine"
    "appengine/urlfetch"
)

const (
    GAE_DEV_SERVER = "Development/"
    GAE_PROD_SERVER = "Google App Engine/"
    PROXY_BASE_URL_COOKIE = "proxy-base-url"
)

func init() {
    http.HandleFunc("/", rootHandler)
}

func isAppEngine() bool {
    serverEnv := os.Getenv("SERVER_SOFTWARE")
    return strings.HasPrefix(serverEnv, GAE_DEV_SERVER) || strings.HasPrefix(serverEnv, GAE_PROD_SERVER)
}

func fetch(w http.ResponseWriter, baseUrlStr string, u *url.URL, ctx *appengine.Context) {
    baseUrl, err := url.Parse(baseUrlStr)
    if err != nil {
        (*ctx).Warningf("Failed to parse base-url %s: %s", baseUrlStr, err)
        w.WriteHeader(http.StatusInternalServerError)
        return
    }

    resolvedUrl := baseUrl.ResolveReference(u)

    (*ctx).Infof("baseUrl = %s", baseUrl)
    (*ctx).Infof("requestUrl = %s", u)
    (*ctx).Infof("resolvedUrl = %s", resolvedUrl)

    (*ctx).Infof("Fetching %s", resolvedUrl)

    client := urlfetch.Client(*ctx)
    resp, err := client.Get(resolvedUrl.String())
    if err != nil {
        fmt.Fprintf(w, "Error fetching %s: %s\n", resolvedUrl, err)
        w.WriteHeader(http.StatusInternalServerError)
        return
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        fmt.Fprintf(w, "Error fetching %s: %s\n", resolvedUrl, resp.Status)
        w.WriteHeader(resp.StatusCode)
        return
    }

    contentType := resp.Header.Get("Content-Type")

    if strings.HasPrefix(contentType, "image/") {
        w.Header().Add("Content-Type", "image/png")
        err = funkifyImage(contentType, resp.Body, w, ctx)
        if err != nil {
            (*ctx).Errorf("Error converting image %s: %s", resolvedUrl, err)
            // just drop through to try and return the original image
        } else {
            return
        }
    }

    w.Header().Add("Content-Type", contentType)
    _, err = io.Copy(w, resp.Body)
    if err != nil {
        (*ctx).Errorf("Error copying fetch body to response: %s", err);
        w.WriteHeader(http.StatusInternalServerError)
        return
    }
}

// From the docs on http/ServeMux it doesn't look like there is a way to do
// this kind of differentiation via http.HandleFunc, so do it ourselves...
func rootHandler(w http.ResponseWriter, r *http.Request) {
    ctx := appengine.NewContext(r)

    err := r.ParseForm()
    if err != nil {
        ctx.Errorf("Failed to parse form elements")
        w.WriteHeader(http.StatusBadRequest)
        return
    }

    // on appspot, the request URLs include the scheme and host, which we don't
    // want
    r.URL.Scheme = ""
    r.URL.Host = ""

    baseUrlStr := r.Form.Get("__base")
    if baseUrlStr != "" {
        fetchHandler(w, baseUrlStr, ctx)
        u := r.URL
        values := u.Query()
        values.Del("__base")
        u.RawQuery = values.Encode()
        fetch(w, baseUrlStr, r.URL, &ctx)
        return
    }

    if r.URL.Path == "/" {
        mainHandler(w, r)
    } else {
        proxyHandler(w, r)
    }
}

func fetchHandler(w http.ResponseWriter, urlStr string, ctx appengine.Context) {
    ctx.Infof("fetchHandler urlStr arg is %s", urlStr)
    i := strings.LastIndex(urlStr, "/")
    baseUrlStr := urlStr[0:i+1]

    expires := time.Now().Add(time.Minute)
    ctx.Infof("Setting expires to %s", expires)
    cookie := http.Cookie{Name:PROXY_BASE_URL_COOKIE, Value:baseUrlStr, Expires:expires}
    ctx.Infof("Setting cookie to %s", cookie.String())
    http.SetCookie(w, &cookie)
}

func mainHandler(w http.ResponseWriter, r *http.Request) {
    fmt.Fprintf(w, "<html>\n%s", headerHTML)
    fmt.Fprintf(w, "<body>\n%s", formHTML)
    fmt.Fprintf(w, "%s\n", iframeHTML)
    fmt.Fprintf(w, "</body>\n</html>\n")
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
    ctx := appengine.NewContext(r)

    c, err := r.Cookie(PROXY_BASE_URL_COOKIE)
    if err != nil {
        ctx.Warningf("No %s cookie in request for %s", PROXY_BASE_URL_COOKIE, r.URL)
        w.WriteHeader(http.StatusInternalServerError)
        return
    }

    fetch(w, c.Value, r.URL, &ctx)
}

func funkifyImage(contentType string, r io.Reader, w io.Writer, ctx *appengine.Context) error {
    var decoder func(io.Reader) (image.Image, error)

    if contentType == "image/gif" {
        decoder = gif.Decode
    } else if contentType == "image/jpeg" {
        decoder = jpeg.Decode
    } else if contentType == "image/png" {
        decoder = png.Decode
    } else {
        return fmt.Errorf("Unsupported content-type: %s", contentType)
    }

    img, err := decoder(r)
    if err != nil {
        return err
    }

    // reference: http://blog.golang.org/2011/09/go-imagedraw-package.html
    b := img.Bounds()
    m := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
    draw.Draw(m, m.Bounds(), img, b.Min, draw.Src)

    // RGBA values are stored as uint32s, but they are only scaled up to 16 bits
    const MAXVAL = 1<<16 - 1

    // invert each pixel
    for x := b.Min.X; x < b.Max.X; x++ {
        for y := b.Min.Y; y < b.Max.Y; y++ {
            red, green, blue, alpha := m.At(x, y).RGBA()
            c := color.RGBA64{MAXVAL - uint16(red), MAXVAL - uint16(green),
                MAXVAL - uint16(blue), uint16(alpha)}
            m.Set(x, y, c)
        }
    }

    return png.Encode(w, m)
}

const headerHTML = `
<head>
    <title>FunkyProxy-Go</title>
</head>
`

const formHTML = `
<script type="text/javascript">
    function fetchit() {
        var s = document.getElementById('input0').value;
        if (s.substring(0, 4) != 'http') {
            s = 'http://' + s;
        }
        console.log(s);

        var myURL = parseURL(s);
        console.log(myURL);

        if (myURL.path == '') {
            myURL.path = '/';
        }

        var arg = encodeURI(myURL.protocol + '://' + myURL.host + '/');
        console.log('__base arg will be: ' + arg);

        if (myURL.query == '') {
            myURL.query = '?__base=' + arg;
        } else {
            myURL.query += '&__base=' + arg;
        }

        document.getElementById('iframe0').src = myURL.path + myURL.query;
        console.log(document.getElementById('iframe0').src);
    }

    // This function creates a new anchor element and uses location
    // properties (inherent) to get the desired URL data. Some String
    // operations are used (to normalize results across browsers).
     
    function parseURL(url) {
        var a =  document.createElement('a');
        a.href = url;
        return {
            source: url,
            protocol: a.protocol.replace(':',''),
            host: a.hostname,
            port: a.port,
            query: a.search,
            params: (function(){
                var ret = {},
                    seg = a.search.replace(/^\?/,'').split('&'),
                    len = seg.length, i = 0, s;
                for (;i<len;i++) {
                    if (!seg[i]) { continue; }
                    s = seg[i].split('=');
                    ret[s[0]] = s[1];
                }
                return ret;
            })(),
            file: (a.pathname.match(/\/([^\/?#]+)$/i) || [,''])[1],
            hash: a.hash.replace('#',''),
            path: a.pathname.replace(/^([^\/])/,'/$1'),
            relative: (a.href.match(/tps?:\/\/[^\/]+(.+)/) || [,''])[1],
            segments: a.pathname.replace(/^\//,'').split('/')
        };
    }
</script>
<div width="100%">
    URL: <input id="input0" type="text" size="60" />
    <input type="button" value="Fetch" onClick="javascript:fetchit()">
</div>
`

const iframeHTML = `
<iframe id="iframe0" border="0" width="100%" height="100%"></iframe>"
`