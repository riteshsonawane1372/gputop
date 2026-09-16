/* gputop site behaviour: theme toggle, copy buttons, install tabs.
   Everything degrades gracefully — the page is fully readable with JS off. */

(function () {
  "use strict";

  /* ---------------------------------------------------------- theme --- */
  /* Persist an explicit choice only. With nothing stored we leave the root
     unstamped so the OS preference wins via prefers-color-scheme. */

  var root = document.documentElement;

  function storedTheme() {
    try {
      return localStorage.getItem("gputop-theme");
    } catch (e) {
      return null; // private window, blocked site data
    }
  }

  function storeTheme(value) {
    try {
      localStorage.setItem("gputop-theme", value);
    } catch (e) {
      /* not fatal — the toggle still works for this page view */
    }
  }

  var saved = storedTheme();
  if (saved === "dark" || saved === "light") {
    root.setAttribute("data-theme", saved);
  }

  var toggle = document.getElementById("theme-toggle");
  if (toggle) {
    toggle.addEventListener("click", function () {
      var prefersDark =
        window.matchMedia &&
        window.matchMedia("(prefers-color-scheme: dark)").matches;
      var current =
        root.getAttribute("data-theme") || (prefersDark ? "dark" : "light");
      var next = current === "dark" ? "light" : "dark";
      root.setAttribute("data-theme", next);
      storeTheme(next);
      toggle.setAttribute(
        "aria-label",
        next === "dark" ? "Switch to light theme" : "Switch to dark theme"
      );
    });
  }

  /* ----------------------------------------------------------- copy --- */

  document.querySelectorAll("[data-copy]").forEach(function (button) {
    button.addEventListener("click", function () {
      var text = button.dataset.copy;
      var done = function () {
        var previous = button.textContent;
        button.textContent = "Copied";
        setTimeout(function () {
          button.textContent = previous;
        }, 1600);
      };

      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(done, fallback);
      } else {
        fallback();
      }

      /* execCommand path for non-secure contexts, where the async
         clipboard API is unavailable. */
      function fallback() {
        var area = document.createElement("textarea");
        area.value = text;
        area.setAttribute("readonly", "");
        area.style.position = "fixed";
        area.style.opacity = "0";
        document.body.appendChild(area);
        area.select();
        try {
          document.execCommand("copy");
          done();
        } catch (e) {
          button.textContent = "Select manually";
        }
        document.body.removeChild(area);
      }
    });
  });

  /* ----------------------------------------------------------- tabs --- */

  var tabs = Array.prototype.slice.call(document.querySelectorAll(".tab"));

  function selectTab(tab) {
    tabs.forEach(function (other) {
      var selected = other === tab;
      other.setAttribute("aria-selected", selected ? "true" : "false");
      var panel = document.getElementById(
        other.getAttribute("aria-controls")
      );
      if (panel) panel.hidden = !selected;
    });
  }

  tabs.forEach(function (tab, index) {
    tab.addEventListener("click", function () {
      selectTab(tab);
    });

    /* Arrow-key navigation, as expected of a real tablist. */
    tab.addEventListener("keydown", function (event) {
      var offset =
        event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
      if (!offset) return;
      event.preventDefault();
      var next = tabs[(index + offset + tabs.length) % tabs.length];
      selectTab(next);
      next.focus();
    });
  });
})();
