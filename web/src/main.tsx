import { createRoot } from "react-dom/client";
import "@fontsource-variable/geist";
import { QueryClientProvider } from "@tanstack/react-query";
import { createBrowserRouter, RouterProvider } from "react-router-dom";
import { App, RouteError } from "./app/App";
import { queryClient } from "./data/query";
import { BrowsePage } from "./pages/BrowsePage";
import { Loading } from "./components/Feedback";
import { AssetOverlayLayout } from "./app/AssetOverlayLayout";
import { LegacyBookmark } from "./app/LegacyBookmark";
import { TagCollectionPage } from "./pages/TagCollectionPage";
import { ReplacePage } from "./pages/ReplacePage";
import { AssetEditPage } from "./pages/AssetEditPage";
import { MinePage } from "./pages/MinePage";
import { AuthorsPage } from "./pages/AuthorsPage";
import { AuthorPage, AuthorProfilePage } from "./pages/AuthorPage";
import { AuthorArchivesPage } from "./pages/AuthorArchivePage";
import { ActivityRankingPage } from "./pages/ActivityRankingPage";
import { SettingsPage } from "./pages/SettingsPage";
import { AIAccessPage } from "./pages/AIAccessPage";
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
    hydrateFallbackElement: <Loading />,
    errorElement: <RouteError />,
    children: [{ errorElement: <RouteError />, children: [
      { path: "/app/*", element: <LegacyBookmark /> },
      { element: <AssetOverlayLayout />, children: [
        { path: "/", element: null }, { path: "/album", element: null }, { path: "/asset/:id", lazy: async () => ({ Component: (await import("./pages/AssetPage")).AssetPage }) },
      ] },
      { path: "/search", element: <BrowsePage /> },
      { path: "/collection/tag/:name", element: <TagCollectionPage /> },
      { path: "/asset/:id/edit", element: <AssetEditPage /> },
      { path: "/asset/:id/replace", element: <ReplacePage /> },
      { path: "/mine", element: <MinePage /> },
      { path: "/data", lazy: async () => ({ Component: (await import("./pages/DataPage")).DataPage }) },
      { path: "/data/review", lazy: async () => ({ Component: (await import("./pages/DataPage")).ReviewPage }) },
      { path: "/data/rankings", element: <ActivityRankingPage /> },
      { path: "/manage", lazy: async () => ({ Component: (await import("./pages/ManagePage")).ManagePage }) },
      { path: "/manage/vocabulary", lazy: async () => ({ Component: (await import("./pages/VocabularyPage")).VocabularyPage }) },
      { path: "/manage/vocabulary/rules", lazy: async () => ({ Component: (await import("./pages/SourceRulesPage")).SourceRulesPage }) },
      { path: "/manage/acquisition", lazy: async () => ({ Component: (await import("./pages/AcquisitionVocabularyPage")).AcquisitionVocabularyPage }) },
      { path: "/manage/files", lazy: async () => ({ Component: (await import("./pages/FileManagementPage")).FileManagementPage }) },
      { path: "/manage/upload", lazy: async () => ({ Component: (await import("./pages/UploadPage")).UploadPage }) },
      { path: "/manage/libraries", lazy: async () => ({ Component: (await import("./pages/LibrariesPage")).LibrariesPage }) },
      { path: "/manage/collections", lazy: async () => ({ Component: (await import("./pages/LibraryPages")).CollectionsPage }) },
      { path: "/manage/tasks", lazy: async () => ({ Component: (await import("./pages/TasksPage")).TasksPage }) },
      { path: "/manage/tasks/:id", lazy: async () => ({ Component: (await import("./pages/TasksPage")).TaskPage }) },
      { path: "/manage/scans/:id", lazy: async () => ({ Component: (await import("./pages/ScanPage")).ScanPage }) },
      { path: "/manage/scans", lazy: async () => ({ Component: (await import("./pages/ScanPage")).ScansPage }) },
      { path: "/manage/author-tasks", lazy: async () => ({ Component: (await import("./pages/AuthorTaskPages")).AuthorTasksPage }) },
      { path: "/manage/author-tasks/:id", lazy: async () => ({ Component: (await import("./pages/AuthorTaskPages")).AuthorTaskPage }) },
      { path: "/manage/trash", lazy: async () => ({ Component: (await import("./pages/LibraryPages")).TrashPage }) },
      { path: "/manage/backup", lazy: async () => ({ Component: (await import("./pages/BackupPage")).BackupPage }) },
      { path: "/authors", element: <AuthorsPage /> },
      { path: "/authors/:id", element: <AuthorPage /> },
      { path: "/authors/:id/profile", element: <AuthorProfilePage /> },
      { path: "/authors/:id/archives", element: <AuthorArchivesPage /> },
      {
        path: "/authors/:id/archives/:archiveId",
        element: <AuthorArchivesPage />,
      },
      { path: "/maintenance", lazy: async () => ({ Component: (await import("./pages/MaintenancePage")).MaintenancePage }) },
      { path: "/maintenance/storage", lazy: async () => ({ Component: (await import("./pages/MaintenancePage")).StorageDetailsPage }) },
      { path: "/maintenance/client-errors", lazy: async () => ({ Component: (await import("./pages/MaintenancePage")).ClientErrorsPage }) },
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
