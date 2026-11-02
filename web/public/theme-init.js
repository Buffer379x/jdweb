// Runs before the first paint, so that a stored choice shows no flash of the other theme.
// It is a file of its own because the CSP allows no inline script.
(function () {
  try {
    var th = localStorage.getItem("jdweb.theme");
    if (th === "dark" || th === "light") document.documentElement.setAttribute("data-theme", th);
    var lg = localStorage.getItem("jdweb.lang");
    var l = lg === "de" || lg === "en" ? lg : (navigator.language || "en").slice(0, 2) === "de" ? "de" : "en";
    document.documentElement.lang = l;
  } catch (e) {}
})();
