// What the explorer page does: asks each seed for its newest block and
// fills in the table. It is a file of its own so that the page can forbid
// scripts written into it (Lockpick findings LP-4SJ838 and LP-WFHY62).
(function () {
  var DOMAIN = "covenantstandard.online";
  var SEEDS = [
    ["Singapore", "singapore"],
    ["Atlanta", "atlanta"],
    ["Los Angeles", "losangeles"],
    ["Toronto", "toronto"],
    ["Amsterdam", "amsterdam"],
    ["Mumbai", "mumbai"]
  ];
  var body = document.getElementById("seeds");
  var summary = document.getElementById("summary");
  var rows = SEEDS.map(function (seed) {
    var url = "https://" + seed[1] + "." + DOMAIN + "/";
    var row = document.createElement("tr");
    function cell(text, className) {
      var td = document.createElement("td");
      td.textContent = text;
      if (className) td.className = className;
      row.appendChild(td);
      return td;
    }
    cell(seed[0]);
    var height = cell("…", "mono");
    var block = cell("…", "mono");
    var races = cell("…", "mono");
    var last = document.createElement("td");
    var link = document.createElement("a");
    link.href = url;
    link.textContent = "Open explorer";
    last.appendChild(link);
    row.appendChild(last);
    body.appendChild(row);
    return { url: url, height: height, block: block, races: races, answer: null };
  });

  function ask(row) {
    var stop = new AbortController();
    var timer = setTimeout(function () { stop.abort(); }, 8000);
    return fetch(row.url + "block", { signal: stop.signal, cache: "no-store" })
      .then(function (reply) { return reply.json(); })
      .then(function (data) {
        if (typeof data.current_height !== "number" || typeof data.block_hash !== "string") throw new Error("unexpected reply");
        row.answer = { height: data.current_height, hash: data.block_hash };
        row.height.textContent = data.current_height.toLocaleString("en");
        row.height.className = "mono";
        row.block.textContent = data.block_hash.slice(0, 16) + "…";
        row.block.title = data.block_hash;
        row.races.textContent = typeof data.races === "number" ? String(data.races) : "–";
      })
      .catch(function () {
        row.answer = null;
        row.height.textContent = "no answer";
        row.height.className = "bad";
        row.block.textContent = "–";
        row.races.textContent = "–";
      })
      .then(function () { clearTimeout(timer); });
  }

  function report() {
    var answered = rows.filter(function (row) { return row.answer; });
    if (answered.length === 0) {
      summary.textContent = "None of the seeds answered just now. They may be restarting, or your connection may be blocking them.";
      summary.className = "bad";
      return;
    }
    var top = Math.max.apply(null, answered.map(function (row) { return row.answer.height; }));
    var atTop = answered.filter(function (row) { return row.answer.height === top; });
    var sameBlock = atTop.every(function (row) { return row.answer.hash === atTop[0].answer.hash; });
    var farBehind = answered.filter(function (row) { return top - row.answer.height > 2; }).length;
    var text = answered.length + " of " + rows.length + " seeds answered. ";
    if (!sameBlock) {
      text += "Seeds at height " + top.toLocaleString("en") + " show different blocks: two blocks are competing, which settles with the next block.";
      summary.className = "quiet";
    } else if (farBehind > 0) {
      text += farBehind + (farBehind === 1 ? " is" : " are") + " more than two blocks behind the others.";
      summary.className = "bad";
    } else {
      text += "They agree, at height " + top.toLocaleString("en") + ".";
      summary.className = "good";
    }
    summary.textContent = text;
  }

  function round() {
    Promise.all(rows.map(ask)).then(report);
  }
  round();
  setInterval(round, 30000);
})();
