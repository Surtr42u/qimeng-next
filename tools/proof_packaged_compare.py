"""Compare only verified official packages over real TCP on an isolated Actions runner."""
from contextlib import ExitStack, contextmanager
import gzip
import hashlib
from http.cookies import SimpleCookie
import json
import os
from pathlib import Path
import platform
import secrets
import signal
import statistics
import struct
import subprocess
import tempfile
import time
import zipfile
import zlib

from proof_packaged_baseline import BASELINE, core, package, request, running

CANDIDATE = "49249ea79c718b251f777b38ab7401acd691f686"
PORTS = {"baseline": 18493, "candidate": 18494}


def sha(body):
    return hashlib.sha256(body).hexdigest()


def verify_static(app, port, candidate):
    urls = core(app)
    stored = {"/" + str(p.relative_to(app/"web")).replace(os.sep, "/") for p in (app/"web").rglob("*.gz")}
    assert stored == ({url + ".gz" for url in urls} if candidate else set())
    out = {}
    for url in urls:
        original = (app/"web"/url[1:]).read_bytes()
        sidecar = (app/"web"/(url[1:]+".gz")).read_bytes() if candidate else original
        status, chosen, body = request(port, url, headers={"Accept-Encoding": "gzip, deflate, br, zstd"})
        assert status == 200 and body == sidecar and int(chosen["content-length"]) == len(body)
        assert gzip.decompress(body) == original if candidate else body == original
        assert chosen.get("content-encoding", "identity") == ("gzip" if candidate else "identity")
        status, identity, raw = request(port, url, headers={"Accept-Encoding": "identity"})
        assert status == 200 and raw == original and "content-encoding" not in identity
        assert chosen["content-type"] == identity["content-type"]
        checks = {"get": status}
        if candidate:
            assert chosen["vary"] == identity["vary"] == "Accept-Encoding"
            assert chosen["etag"] == '"' + sha(body) + '"' and identity["etag"] == '"' + sha(raw) + '"'
            for header, expected in [("", "identity"), ("br", "identity"), ("gzip;q=0, *;q=1", "identity"),
                                     ("identity;q=1, GZIP;q=0.25", "identity"), ("br, GZIP;q=0.25", "gzip"),
                                     ("*;q=0.1, identity;q=0", "gzip"), ("gzip;q=0.7, identity;q=0.2", "gzip")]:
                s, h, b = request(port, url, headers={"Accept-Encoding": header})
                assert s == 200 and h.get("content-encoding", "identity") == expected
                assert b == (body if expected == "gzip" else raw)
            for enc, expected, etag in [("gzip", body, chosen["etag"]), ("identity", raw, identity["etag"])]:
                s, h, b = request(port, url, method="HEAD", headers={"Accept-Encoding": enc})
                assert s == 200 and not b and int(h["content-length"]) == len(expected) and h["etag"] == etag
                for name, value in (("If-None-Match", etag), ("If-Modified-Since", h["last-modified"])):
                    s, h304, b = request(port, url, headers={"Accept-Encoding": enc, name: value})
                    assert s == 304 and not b and h304["vary"] == "Accept-Encoding"
                other = identity["etag"] if enc == "gzip" else chosen["etag"]
                s, _, b = request(port, url, headers={"Accept-Encoding": enc, "If-None-Match": other})
                assert s == 200 and b == expected
            for enc, expected, etag, wire_encoding in [("gzip", body, chosen["etag"], "gzip"),
                                                        ("gzip, identity;q=0", body, chosen["etag"], "gzip"),
                                                        ("identity", raw, identity["etag"], "identity")]:
                ready = {"Accept-Encoding":enc,"Range":"bytes=7-19","If-Range":etag}
                s, h, b = request(port, url, headers=ready)
                assert (s, h["content-range"], b) == (206, f"bytes 7-19/{len(expected)}", expected[7:20])
                assert h.get("content-encoding","identity") == wire_encoding and int(h["content-length"]) == len(b)
                s, h, b = request(port, url, headers={**ready,"If-Range":'"different"'})
                assert s == 200 and b == expected
                for conditional in ["If-Match","If-Unmodified-Since"]:
                    condition = '"different"' if conditional=="If-Match" else "Sun, 06 Nov 1994 08:49:37 GMT"
                    s, h, b = request(port, url, headers={"Accept-Encoding":enc,conditional:condition})
                    assert s == 412 and not b and "content-encoding" not in h
            s, h, b = request(port, url, headers={"Accept-Encoding":"gzip","Range":"bytes=0-2,7-19"})
            assert s == 200 and b == body and h["content-encoding"] == "gzip"
            s, h, _ = request(port, url, headers={"Accept-Encoding": "gzip;q=0, *;q=0"})
            assert s == 406 and h["vary"] == "Accept-Encoding"
            checks.update(HEAD=True, sameValidator304=True, otherRepresentation200=True,
                          rangeIdentity206=True, rangeGzip206=True, gzipMultipartFull200=True,
                          mismatchedIfRangeFull200=True, failedPreconditions412=True,
                          noAcceptable406=True, weightedNegotiation=True)
        out[url] = dict(mime=chosen["content-type"], encoding=chosen.get("content-encoding", "identity"),
                        encodedBytes=len(body), decodedBytes=len(original), encodedSHA256=sha(body),
                        decodedSHA256=sha(original), identitySHA256=sha(raw), checks=checks)
    plain = sorted(p for p in (app/"web").rglob("*") if p.is_file() and p.suffix != ".gz"
                   and "/"+p.relative_to(app/"web").as_posix() not in urls and p.name != "index.html")
    for file in plain:
        url = "/"+file.relative_to(app/"web").as_posix()
        s, h, b = request(port, url, headers={"Accept-Encoding": "br, gzip"})
        assert s == 200 and "content-encoding" not in h and b == file.read_bytes()
    return dict(core=out, unencodedOtherPublicFiles=len(plain), exactlyExpectedSidecars=sorted(stored))


def seed(port, admin):
    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))
    pixels = b"".join(b"\0" + bytes([18, 70, 119])*64 for _ in range(48))
    png = (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", 64, 48, 8, 2, 0, 0, 0))
           + chunk(b"IDAT", zlib.compress(pixels)) + chunk(b"IEND", b""))
    base = f"http://127.0.0.1:{port}"
    headers = {"Authorization": "Bearer "+admin, "Origin": base, "Accept-Encoding": "br, gzip"}
    def post(path, key, data):
        s, h, b = request(port, "/api/v1"+path, method="POST", headers={**headers, "Idempotency-Key": key,
                           "Content-Type": "application/json"}, body=json.dumps(data).encode())
        assert 200 <= s < 300, (path, s)
        assert h["cache-control"] == "no-store" and "content-encoding" not in h
        return json.loads(b)
    library = post("/libraries", "fixture-library", {"name":"Synthetic library","kind":"regular"})
    u = post("/uploads", "fixture-create", {"libraryId":library["id"],"mode":"ordinary",
             "fileName":"synthetic-entry.png","mimeType":"image/png","size":len(png),"expectedHash":sha(png)})
    upload = "/uploads/"+u["id"]
    s, _, _ = request(port, "/api/v1"+upload+"/content", method="PUT", headers={**headers,
                      "Content-Type":"application/octet-stream","Upload-Offset":"0"}, body=png)
    assert 200 <= s < 300, s
    asset = post(upload+"/complete", "fixture-complete", {})["asset"]
    url = "/api/v1/assets/"+asset["id"]+"/content"
    s, h, b = request(port, url, headers={**headers, "Range":"bytes=6-31"})
    assert s == 206 and b == png[6:32] and h["content-range"] == f"bytes 6-31/{len(png)}"
    assert "content-encoding" not in h and h["cache-control"] == "private, no-cache"
    authenticated_range = h["content-range"]
    s, h, _ = request(port, url, headers={"Accept-Encoding":"gzip"})
    assert s == 401 and "no-store" in h["cache-control"]
    s, h, _ = request(port, "/api/v1/sessions", method="POST", headers={"Origin":base,"Content-Type":"application/json"},
                      body=json.dumps({"token":admin,"label":"Synthetic browser"}).encode())
    assert s == 201 and h["cache-control"] == "no-store"
    cookie = SimpleCookie(); cookie.load(h["set-cookie"])
    return cookie["qimeng_session"].value, dict(ordinaryUpload=True, sourceSHA256=sha(png), originalSize=len(png),
           authenticatedRange=authenticated_range, unauthorizedOriginal=401, sessionCache="no-store")


@contextmanager
def shape(enabled):
    if not enabled:
        yield
        return
    def tc(*args):
        subprocess.run(["sudo","tc",*args], check=True)
    tc("qdisc","add","dev","lo","root","handle","1:","prio","bands","3","priomap",*(["0"]*16))
    try:
        for band, direction in ((2,"sport"),(3,"dport")):
            tc("qdisc","add","dev","lo","parent",f"1:{band}","handle",f"{band}0:","netem","delay","20ms","rate","10mbit")
            for port in PORTS.values():
                tc("filter","add","dev","lo","protocol","ip","parent","1:","prio","1","u32","match","ip","protocol","6","0xff",
                   "match","ip",direction,str(port),"0xffff","flowid",f"1:{band}")
        subprocess.run(["tc","-s","qdisc","show","dev","lo"], check=True)
        yield
        subprocess.run(["tc","-s","qdisc","show","dev","lo"], check=True)
    finally:
        tc("qdisc","del","dev","lo","root")


@contextmanager
def capture(path):
    expression = "ip and tcp and (" + " or ".join(f"src port {p}" for p in PORTS.values()) + ")"
    with tempfile.TemporaryFile(mode="w+b") as log:
        process = subprocess.Popen(["sudo","tcpdump","-Z",os.environ["USER"],"-i","lo","-s","96","-U","-w",str(path),expression],
                                   stderr=log, start_new_session=True)
        try:
            for _ in range(80):
                if process.poll() is not None:
                    log.seek(0); raise RuntimeError(log.read().decode())
                if path.exists() and path.stat().st_size >= 24:
                    break
                request(PORTS["baseline"], "/healthz")
                time.sleep(.05)
            else:
                raise TimeoutError("Capture did not open")
            yield
        finally:
            time.sleep(.2)
            subprocess.run(["sudo","kill","-INT","--",str(-process.pid)], check=False)
            process.wait(timeout=8)
            log.seek(0)
            message = log.read().decode()
            print(message)
            assert "0 packets dropped by kernel" in message, message


def read_packets(path):
    data = path.read_bytes()
    assert data[:4] == b"\xd4\xc3\xb2\xa1" and struct.unpack_from("<I",data,20)[0] == 1
    i = 24
    while i < len(data):
        sec,us,captured,original = struct.unpack_from("<4I",data,i); i += 16
        frame = data[i:i+captured]; i += captured
        assert frame[12:14] == b"\x08\0"
        ip = frame[14:]; tcp = ip[(ip[0]&15)*4:]
        port = int.from_bytes(tcp[:2],"big"); length = int.from_bytes(ip[2:4],"big") or original-14
        yield (sec*1000+us/1000,port,length-(ip[0]&15)*4-(tcp[12]>>4)*4,length)


def measured(packets, sample):
    selected = [p for p in packets if p[1] == PORTS[sample["variant"]] and sample["startEpochMs"] <= p[0] < sample["endEpochMs"]]
    until = [p for p in selected if p[0] < sample["actionEpochMs"]]
    def count(rows):
        return {"tcpPayloadBytes":sum(p[2] for p in rows), "ipv4Bytes":sum(p[3] for p in rows), "downstreamPackets":len(rows)}
    sample.update(throughNetworkIdle=count(selected), throughActionable=count(until))
    assert sample["throughNetworkIdle"]["tcpPayloadBytes"] > 0


def compare():
    assert os.environ["GITHUB_ACTIONS"] == "true" and platform.system() == "Linux"
    result={"package":{}, "network":{}, "browser":[]}
    with tempfile.TemporaryDirectory() as temporary, ExitStack() as stack:
        work = Path(temporary)
        cookies = {}
        web_digests = {}
        for label, commit in (("baseline",BASELINE),("candidate",CANDIDATE)):
            directory = Path("build/baseline") if label=="baseline" else Path("build/published/qimeng-next-package-linux-amd64")
            archive, app = package(directory, work/label/"pkg", commit)
            port = PORTS[label]; admin = secrets.token_urlsafe(40)
            stack.enter_context(running(app,work/label/"data",port,admin))
            cookies[label], fixture = seed(port,admin)
            files = [p for p in app.rglob("*") if p.is_file()]
            static = verify_static(app,port,label=="candidate")
            web_digests[label] = {p.relative_to(app/"web").as_posix():sha(p.read_bytes())
                                  for p in (app/"web").rglob("*") if p.is_file() and p.suffix!=".gz"}
            result["package"][label] = dict(commit=commit,zipBytes=archive.stat().st_size,
                extractedFileBytes=sum(p.stat().st_size for p in files), executableBytes=(app/"qimeng-next").stat().st_size,
                webLogicalBytes=sum(p.stat().st_size for p in (app/"web").rglob("*") if p.is_file()),
                webDiskAllocatedBytes=sum(p.stat().st_blocks*512 for p in (app/"web").rglob("*") if p.is_file()),
                packageDiskAllocatedBytes=sum(p.stat().st_blocks*512 for p in files),
                sidecarBytes=sum(p.stat().st_size for p in (app/"web").rglob("*.gz")), checksumsVerified=True,
                fixture=fixture, **static)
        assert web_digests["baseline"] == web_digests["candidate"]
        result["identicalExistingWebFiles"] = len(web_digests["baseline"])
        result["platformPackages"] = {}
        canonical = result["package"]["candidate"]["core"]
        for archive in sorted(Path("build/published").glob("*/*.zip")):
            name = archive.stem
            assert sha(archive.read_bytes()) == Path(str(archive)+".sha256").read_text().split()[0]
            with zipfile.ZipFile(archive) as pkg:
                src = json.loads(pkg.read(name+"/SOURCE.json"))
                assert src["sourceCommit"] == CANDIDATE
                checks = {path: digest for digest,path in (line.split("  ",1) for line in pkg.read(name+"/SHA256SUMS").decode().splitlines())}
                pairs = {}
                for url, expected in canonical.items():
                    relative = "web"+url
                    raw = pkg.read(name+"/"+relative); compressed = pkg.read(name+"/"+relative+".gz")
                    assert sha(raw) == checks[relative] == expected["decodedSHA256"]
                    assert sha(compressed) == checks[relative+".gz"] and gzip.decompress(compressed) == raw
                    assert compressed[3] == 0 and compressed[4:8] == bytes(4)
                    pairs[url] = {"bytes":len(compressed), "sha256":sha(compressed)}
                result["platformPackages"][src["target"]] = {"zipBytes":archive.stat().st_size,"core":pairs}
        assert len(result["platformPackages"]) == 3
        result["network"]["loopback"] = "native loopback HTTP/1.1"
        result["network"]["restricted"] = "Linux loopback tc netem; two directions each 20 ms one-way, 10 Mbit/s; 40 ms nominal RTT"
        for network in result["network"]:
            with shape(network=="restricted"), capture(work/(network+".pcap")):
                env = {**os.environ,"PROOF_COOKIES":json.dumps(cookies),"PROOF_PORTS":json.dumps(PORTS),"PROOF_NETWORK":network}
                subprocess.run(["node","tests/proof-packaged-traffic.mjs"],cwd="web",env=env,check=True,timeout=330)
            samples = json.loads(Path(f"build/browser-static-{network}.json").read_text())
            packets = list(read_packets(work/(network+".pcap")))
            for sample in samples:
                measured(packets,sample)
            result["browser"].extend(samples)
    result["medians"] = []
    for network in result["network"]:
        for cache in ("cold","warm"):
            for variant in PORTS:
                group = [s for s in result["browser"] if (s["network"],s["cache"],s["variant"]) == (network,cache,variant)]
                assert len(group) == 4
                result["medians"].append(dict(network=network,cache=cache,variant=variant,
                    actionableMs=statistics.median(s["actionableMs"] for s in group),
                    tcpPayloadBytes=statistics.median(s["throughNetworkIdle"]["tcpPayloadBytes"] for s in group),
                    ipv4Bytes=statistics.median(s["throughNetworkIdle"]["ipv4Bytes"] for s in group)))
    Path("build/static-comparison.json").write_text(json.dumps(result,indent=2)+"\n")
    print(json.dumps({"package":result["package"],"medians":result["medians"]},indent=2))


if __name__ == "__main__":
    compare()
