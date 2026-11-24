(() => {
  "use strict";

  const mount = document.getElementById("graphiql");
  if (!mount || !window.React || !window.ReactDOM || !window.GraphiQL) return;

  const fetcher = window.GraphiQL.createFetcher({ url: mount.dataset.endpoint });
  const root = window.ReactDOM.createRoot(mount);
  root.render(window.React.createElement(window.GraphiQL, { fetcher }));
})();
