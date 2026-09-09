import { createRoot } from "react-dom/client";
import { App } from "./App.tsx";
import "./workspace.css";
import "./workspace-responsive.css";
createRoot(document.getElementById("root")!).render(<App />);
