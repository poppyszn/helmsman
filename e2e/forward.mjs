// Forwards ports on localhost to other containers, so the browser and Node in the test container can
// use the same addresses the dev stack hands out (localhost:5173 for the web app, localhost:8090 for
// fake-github). It replaces socat, which would need an apt-get in the image.
import net from "node:net";

const routes = [
  [5173, "web", 5173],
  [8090, "fake-github", 8090],
];

for (const [port, host, target] of routes) {
  net
    .createServer((client) => {
      const upstream = net.connect(target, host);
      client.pipe(upstream).pipe(client);
      const close = () => {
        client.destroy();
        upstream.destroy();
      };
      client.on("error", close);
      upstream.on("error", close);
    })
    .listen(port, "127.0.0.1");
}
