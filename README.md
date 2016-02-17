Copy website and serve it statically, using golang.

Go build then run. If some urls in javascript or flash are not correctly converted, just manually copy the files to the static dir then edit the filename according to the Url2Filename function.

Usage: copyweb [dir(path to save static files)] [http://weburl(client) or port number(server)]

Example: copyweb mysite http://www.mysite.com  or copyweb mysite 8080
