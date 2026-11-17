import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { createBrowserRouter, RouterProvider } from "react-router-dom";
import { App, RouteError } from "./app/App";
import { queryClient } from "./data/query";
import { BrowsePage } from "./pages/BrowsePage";
import { HomePage } from "./pages/HomePage";
import { VocabularyPage } from "./pages/VocabularyPage";
import { ReplacePage } from "./pages/ReplacePage";
import { ScanPage } from './pages/ScanPage';
import { AuthorTasksPage, AuthorTaskPage } from './pages/AuthorTaskPages';
import { AssetPage } from "./pages/AssetPage";
import { MinePage } from "./pages/MinePage";
import { AuthorsPage, AuthorPage } from "./pages/AuthorsPage";
import { AuthorArchivesPage } from './pages/AuthorArchivePage';
import { ManagePage } from "./pages/ManagePage";
import { UploadPage } from "./pages/UploadPage";
import { TasksPage, TaskPage } from "./pages/TasksPage";
import {
  LibrariesPage,
  CollectionsPage,
  TrashPage,
  BackupPage,
} from "./pages/LibraryPages";
import { DataPage, ReviewPage, MaintenancePage } from "./pages/DataPage";
import { SettingsPage } from "./pages/SettingsPage";
import "../../ui/react/tokens.css";
import "../../ui/react/glass.css";
import "./styles/app.css";
import "./styles/charts.css";
import "./styles/preferences.css";
import "./styles/detail.css";
import './styles/profiles.css';

const router = createBrowserRouter([
  {
    element: <App />,
    errorElement: <RouteError />,
    children: [
      { path: "/", element: <HomePage /> },
      { path: "/album", element: <BrowsePage /> },
      { path: "/asset/:id", element: <AssetPage /> },
      { path: "/asset/:id/replace", element: <ReplacePage /> },
      { path: "/mine", element: <MinePage /> },
      { path: "/data", element: <DataPage /> },
      { path: "/data/review", element: <ReviewPage /> },
      { path: "/manage", element: <ManagePage /> },
      { path: "/manage/vocabulary", element: <VocabularyPage /> },
      { path: "/manage/upload", element: <UploadPage /> },
      { path: "/manage/libraries", element: <LibrariesPage /> },
      { path: "/manage/collections", element: <CollectionsPage /> },
      { path: "/manage/tasks", element: <TasksPage /> },
      { path: "/manage/tasks/:id", element: <TaskPage /> },
      { path: '/manage/scans/:id', element: <ScanPage /> },
      { path: '/manage/author-tasks', element: <AuthorTasksPage /> },
      { path: '/manage/author-tasks/:id', element: <AuthorTaskPage /> },
      { path: "/manage/trash", element: <TrashPage /> },
      { path: "/manage/backup", element: <BackupPage /> },
      { path: "/authors", element: <AuthorsPage /> },
      { path: "/authors/:id", element: <AuthorPage /> },
      { path: '/authors/:id/archives', element: <AuthorArchivesPage /> },
      { path: '/authors/:id/archives/:archiveId', element: <AuthorArchivesPage /> },
      { path: "/maintenance", element: <MaintenancePage /> },
      { path: "/settings", element: <SettingsPage /> },
    ],
  },
]);
createRoot(document.getElementById("root")!).render(
  <QueryClientProvider client={queryClient}>
    <RouterProvider router={router} />
  </QueryClientProvider>,
);
if ("serviceWorker" in navigator && import.meta.env.PROD)
  window.addEventListener("load", () => {
    void navigator.serviceWorker.register("/sw.js").catch(() => {
      /* An unavailable app-shell cache does not change online data behavior. */
    });
  });
