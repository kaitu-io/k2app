import { BrowserRouter, Routes, Route } from "react-router-dom";
import { ServiceReadiness } from "./components/ServiceReadiness";

function Home() {
  return (
    <div className="flex min-h-screen items-center justify-center">
      <h1 className="text-4xl font-bold">Kaitu.io 开途</h1>
    </div>
  );
}

export default function App() {
  return (
    <BrowserRouter>
      <ServiceReadiness>
        <Routes>
          <Route path="/" element={<Home />} />
        </Routes>
      </ServiceReadiness>
    </BrowserRouter>
  );
}
