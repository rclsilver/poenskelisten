// Service worker: shows the offline page when a page can't be loaded, and
// displays push notifications. It never caches API answers or anything
// personal: only the offline page and the static files it needs.

// Incrementing OFFLINE_VERSION will kick off the install event and force
// previously cached resources to be updated from the network.
const OFFLINE_VERSION = 2;
const CACHE_NAME = 'poenskelisten-cache-v' + OFFLINE_VERSION;
// Page returned from the cache when a navigation fails while offline.
const OFFLINE_URL = '/offline';
// The offline page and what it loads.
const urlsToCache = [
    OFFLINE_URL,
    '/manifest.json',
    '/css/app.css',
    '/js/theme.js',
    '/js/core.js',
    '/js/session.js',
    '/js/offline.js',
    '/json/lang-en.json',
    '/json/lang-fr.json',
    '/assets/illustrations/book.svg',
    '/assets/favicons/favicon.ico',
    '/assets/favicons/favicon-32x32.png'
];

self.addEventListener('install', (event) => {
    event.waitUntil((async () => {
        const cache = await caches.open(CACHE_NAME);
        // Setting {cache: 'reload'} in the new request will ensure that the response
        // isn't fulfilled from the HTTP cache; i.e., it will be from the network.
        for(var i = 0; i < urlsToCache.length; i++) {
            await cache.add(new Request(urlsToCache[i], {cache: 'reload'}));
        }
    })());
    // Take over from an older worker without waiting for every tab to close.
    self.skipWaiting();
});

self.addEventListener('activate', (event) => {
    event.waitUntil((async () => {
        // Enable navigation preload if it's supported.
        if ('navigationPreload' in self.registration) {
            await self.registration.navigationPreload.enable();
        }

        // Purge caches from previous versions so bumping OFFLINE_VERSION
        // actually takes effect.
        const cacheKeys = await caches.keys();
        await Promise.all(
            cacheKeys
                .filter((key) => key.startsWith('poenskelisten-cache') && key !== CACHE_NAME)
                .map((key) => caches.delete(key))
        );
    })());

    // Tell the active service worker to take control of the page immediately.
    self.clients.claim();
});

self.addEventListener('fetch', (event) => {
    const url = new URL(event.request.url);

    // Pages: network first, the offline page when the network is down.
    if (event.request.mode === 'navigate') {
        event.respondWith((async () => {
            try {
                // First, try to use the navigation preload response if it's supported.
                const preloadResponse = await event.preloadResponse;
                if (preloadResponse) {
                    return preloadResponse;
                }

                const networkResponse = await fetch(event.request);
                return networkResponse;
            } catch (error) {
                // catch is only triggered if an exception is thrown, which is likely
                // due to a network error.
                // If fetch() returns a valid HTTP response with a response code in
                // the 4xx or 5xx range, the catch() will NOT be called.
                const cache = await caches.open(CACHE_NAME);
                const cachedResponse = await cache.match(OFFLINE_URL);
                return cachedResponse;
            }
          })());
        return;
    }

    // The offline page's own files: network first so they stay current,
    // the cached copy when offline. Nothing else is ever served from cache.
    if (event.request.method === 'GET' && url.origin === self.location.origin && urlsToCache.includes(url.pathname)) {
        event.respondWith((async () => {
            try {
                return await fetch(event.request);
            } catch (error) {
                const cache = await caches.open(CACHE_NAME);
                const cachedResponse = await cache.match(url.pathname);
                if (cachedResponse) {
                    return cachedResponse;
                }
                throw error;
            }
        })());
    }
});

self.addEventListener('notificationclose', event => {

    try {

        const notification = event.notification;
        const primaryKey = notification.data.primaryKey;

        console.log('Closed notification: ' + primaryKey);

    } catch(e) {
        console.log("Failed to click notfication. Error: " + e)
    }
});

self.addEventListener('notificationclick', event => {

    try {

        const notification = event.notification;
        const primaryKey = notification.data.primaryKey;
        const url = notification.data.url;
        const action = event.action;

        if (action === 'close') {
            notification.close();
        } else {
            event.waitUntil(clients.openWindow(self.location.origin + url));
            notification.close();
        }

        console.log('Clicked notification: ' + primaryKey);

    } catch(e) {
        console.log("Failed to click notfication. Error: " + e)
    }

    // TODO 5.3 - close all notifications when one is clicked

});

self.addEventListener('push', function(event) {

    if (!(self.Notification && self.Notification.permission === "granted")) {
        console.log("Notification permission not given.")
        return;
    }

    console.log("Pushing notification.")

    try {

        let jsonData = event.data?.json() ?? {
            category: "general",
            title: "Error",
            body: "An error occured"
        };

        let url;
        let action;

        if(jsonData.category == "achievement") {
            url = "/achievements"
            action = "Check out"
        } else if(jsonData.category == "news") {
            url = "/"
            action = "Read"
        } else {
            url = "/"
            action = "Visit"
        }

        const options = {
            body: jsonData.body,
            icon: '/assets/logos/logo-384x384.png',
            badge: '/assets/logos/logo-mono-96x96.png',
            vibrate: [100, 50, 100],
            data: {
                dateOfArrival: Date.now(),
                primaryKey: 1,
                url: url
            },
            actions: [
                {action: 'explore', title: action,
                    icon: '/assets/check.svg'
                },
                {action: 'close', title: 'Close',
                    icon: '/assets/x.svg'
                },
            ],
            tag: 'Message'
        };

        event.waitUntil(
            self.registration.showNotification(jsonData.title, options)
        );

    } catch(e) {
        console.log("Failed to push notfication. Error: " + e)
    }

});
