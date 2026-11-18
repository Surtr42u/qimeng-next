import { Outlet, useLocation } from "react-router-dom";
import { HomePage } from "../pages/HomePage";
import { AlbumsView } from "../pages/BrowsePage";
import { backdropLocation, isAssetDetail } from "../lib/detail-navigation";

// The original shell keeps the active list mounted beneath the independently scrolling detail.
export function AssetOverlayLayout() {
  const location = useLocation();
  const open = isAssetDetail(location.pathname);
  const backdrop = open ? backdropLocation(location.state) : location;
  return <>
    <div className="overlay-backdrop" inert={open}>
      {backdrop.pathname === "/album" ? <AlbumsView backdropSearch={backdrop.search} /> : <HomePage backdropSearch={backdrop.search} />}
    </div>
    <Outlet />
  </>;
}
