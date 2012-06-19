funkyproxy-go
=============

This is just a simple exercise for me to learn some Go.

This proxy will invert the colors for any image, but only if the image is
actually requested through the app.  So a web page like this will work:

<html>
<body>
<img src="/images/foo.gif">
</body>
</html>


But not a web page that specifies a host in the img url, like this:

<html>
<body>
<img src="http://images.mycompany.com/foo.gif">
</body>
</html>


Unfortunately most big well-know sites fall into the latter category and
thus don't work.  Try smaller sites like personal website or local businesses.
Here are a few that I found to work decently:

www.gnu.org
www.cdc.gov
www.suninmybelly.com
