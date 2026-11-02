import { render } from "preact";
import "./styles/index.css";
import { initLocalPrefs } from "./store/app.ts";
import "./store/errors.ts";
import { App } from "./views/App.tsx";

initLocalPrefs();
render(<App />, document.getElementById("app")!);
