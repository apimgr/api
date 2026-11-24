(() => {
  "use strict";

  const mount = document.getElementById("swagger-ui");
  const render = window.SwaggerUIBundle;
  if (!mount || typeof render !== "function") return;

  render({
    dom_id: "#swagger-ui",
    url: mount.dataset.specUrl,
    presets: [render.presets.apis, window.SwaggerUIStandalonePreset],
    layout: "StandaloneLayout",
    deepLinking: true,
    validatorUrl: null,
  });
})();
