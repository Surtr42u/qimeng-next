import { createRoot } from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { createBrowserRouter, RouterProvider } from 'react-router-dom';
import { App, RouteError } from './app/App';
import { queryClient } from './data/query';
import { BrowsePage } from './pages/BrowsePage';
import { HomePage } from './pages/HomePage';
import { AssetPage } from './pages/AssetPage';
import { MinePage } from './pages/MinePage';
import { AuthorsPage, AuthorPage } from './pages/AuthorsPage';
import { ManagePage } from './pages/ManagePage';
import { UploadPage } from './pages/UploadPage';
import { TasksPage, TaskPage } from './pages/TasksPage';
import { LibrariesPage, CollectionsPage, TrashPage, BackupPage } from './pages/LibraryPages';
import { DataPage, MaintenancePage } from './pages/DataPage';
import { SettingsPage } from './pages/SettingsPage';
import '../../ui/react/tokens.css';
import '../../ui/react/glass.css';
import './styles/app.css';

const router = createBrowserRouter([{ element: <App />, errorElement: <RouteError />, children: [
  { path: '/', element: <HomePage /> },
  { path: '/album', element: <BrowsePage /> },
  { path: '/asset/:id', element: <AssetPage /> },
  { path: '/mine', element: <MinePage /> },
  { path: '/data', element: <DataPage /> },
  { path: '/manage', element: <ManagePage /> },
  { path: '/manage/upload', element: <UploadPage /> },
  { path: '/manage/libraries', element: <LibrariesPage /> },
  { path: '/manage/collections', element: <CollectionsPage /> },
  { path: '/manage/tasks', element: <TasksPage /> },
  { path: '/manage/tasks/:id', element: <TaskPage /> },
  { path: '/manage/trash', element: <TrashPage /> },
  { path: '/manage/backup', element: <BackupPage /> },
  { path: '/authors', element: <AuthorsPage /> },
  { path: '/authors/:id', element: <AuthorPage /> },
  { path: '/maintenance', element: <MaintenancePage /> },
  { path: '/settings', element: <SettingsPage /> },
] }]);
createRoot(document.getElementById('root')!).render(<QueryClientProvider client={queryClient}><RouterProvider router={router} /></QueryClientProvider>);
if ('serviceWorker' in navigator && import.meta.env.PROD) window.addEventListener('load', () => {
  void navigator.serviceWorker.register('/sw.js').catch(() => { /* An unavailable app-shell cache does not change online data behavior. */ });
});
