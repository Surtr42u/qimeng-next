import { createRoot } from "react-dom/client";
import "@fontsource-variable/geist";
import { QueryClientProvider } from "@tanstack/react-query";
import { createBrowserRouter, RouterProvider } from "react-router-dom";
import { App, RouteError } from "./app/App";
import { queryClient } from "./data/query";
import { BrowsePage } from "./pages/BrowsePage";
import { AssetOverlayLayout } from "./app/AssetOverlayLayout";
import { VocabularyPage } from "./pages/VocabularyPage";
import { ReplacePage } from "./pages/ReplacePage";
import { ScanPage, ScansPage } from "./pages/ScanPage";
import { AuthorTasksPage, AuthorTaskPage } from "./pages/AuthorTaskPages";
import { AssetPage } from "./pages/AssetPage";
import { AssetEditPage } from "./pages/AssetEditPage";
import { MinePage } from "./pages/MinePage";
import { AuthorsPage } from "./pages/AuthorsPage";
import { AuthorPage, AuthorProfilePage } from "./pages/AuthorPage";
import { AuthorArchivesPage } from "./pages/AuthorArchivePage";
import { ManagePage } from "./pages/ManagePage";
import { UploadPage } from "./pages/UploadPage";
import { TasksPage, TaskPage } from "./pages/TasksPage";
import {
  LibrariesPage,
  CollectionsPage,
  TrashPage,
  BackupPage,
} from "./pages/LibraryPages";
import { DataPage, ReviewPage } from "./pages/DataPage";
import { MaintenancePage, StorageDetailsPage, ClientErrorsPage } from "./pages/MaintenancePage";
import { ActivityRankingPage } from "./pages/ActivityRankingPage";
import { SettingsPage } from "./pages/SettingsPage";
import { AIAccessPage } from "./pages/AIAccessPage";
import { AcquisitionVocabularyPage } from "./pages/AcquisitionVocabularyPage";
import { FileManagementPage } from "./pages/FileManagementPage";
import "../../ui/react/tokens.css";
import "./styles/app.css";
import "./styles/charts.css";
import "./styles/preferences.css";
import "./styles/detail.css";
import "./styles/profiles.css";
import "./styles/legacy-base.css";
import "./styles/legacy-ui.css";
import "./styles/legacy-adapter.css";

const router = createBrowserRouter([
  {
    element: <App />,
    errorElement: <RouteError />,
    children: [{ errorElement: <RouteError />, children: [
      { element: <AssetOverlayLayout />, children: [
        { path: "/", element: null }, { path: "/album", element: null }, { path: "/asset/:id", element: <AssetPage /> },
      ] },
      { path: "/search", element: <BrowsePage /> },
      { path: "/asset/:id/edit", element: <AssetEditPage /> },
      { path: "/asset/:id/replace", element: <ReplacePage /> },
      { path: "/mine", element: <MinePage /> },
      { path: "/data", element: <DataPage /> },
      { path: "/data/review", element: <ReviewPage /> },
      { path: "/data/rankings", element: <ActivityRankingPage /> },
      { path: "/manage", element: <ManagePage /> },
      { path: "/manage/vocabulary", element: <VocabularyPage /> },
      { path: "/manage/acquisition", element: <AcquisitionVocabularyPage /> },
      { path: "/manage/files", element: <FileManagementPage /> },
      { path: "/manage/upload", element: <UploadPage /> },
      { path: "/manage/libraries", element: <LibrariesPage /> },
      { path: "/manage/collections", element: <CollectionsPage /> },
      { path: "/manage/tasks", element: <TasksPage /> },
      { path: "/manage/tasks/:id", element: <TaskPage /> },
      { path: "/manage/scans/:id", element: <ScanPage /> },
      { path: "/manage/scans", element: <ScansPage /> },
      { path: "/manage/author-tasks", element: <AuthorTasksPage /> },
      { path: "/manage/author-tasks/:id", element: <AuthorTaskPage /> },
      { path: "/manage/trash", element: <TrashPage /> },
      { path: "/manage/backup", element: <BackupPage /> },
      { path: "/authors", element: <AuthorsPage /> },
      { path: "/authors/:id", element: <AuthorPage /> },
      { path: "/authors/:id/profile", element: <AuthorProfilePage /> },
      { path: "/authors/:id/archives", element: <AuthorArchivesPage /> },
      {
        path: "/authors/:id/archives/:archiveId",
        element: <AuthorArchivesPage />,
      },
      { path: "/maintenance", element: <MaintenancePage /> },
      { path: "/maintenance/storage", element: <StorageDetailsPage /> },
      { path: "/maintenance/client-errors", element: <ClientErrorsPage /> },
      { path: "/settings", element: <SettingsPage /> },
      { path: "/settings/ai", element: <AIAccessPage /> },
    ] }],
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
