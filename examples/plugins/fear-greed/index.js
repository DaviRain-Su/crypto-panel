// Fear & Greed: one allowlisted host, one chart. The manifest names
// api.alternative.me; a fetch to any other host is refused by the shell and
// the refusal is what the plugin sees as an exception.

const URL = "https://api.alternative.me/fng/?limit=30";

export function page({ name }) {
  let res;
  try {
    res = host.fetch(URL);
  } catch (err) {
    return {
      title: "Fear & Greed",
      sections: [{ text: "The shell refused the request: " + String(err.message) }],
    };
  }
  if (!res.ok) {
    return {
      title: "Fear & Greed",
      sections: [{ text: "The API answered " + res.status + "." }],
    };
  }
  const data = JSON.parse(res.body).data;
  const points = data
    .slice()
    .reverse()
    .map((d) => ({
      label: new Date(Number(d.timestamp) * 1000).toISOString().slice(5, 10),
      value: Number(d.value),
    }));
  const latest = data[0];
  return {
    title: "Fear & Greed",
    subtitle: "30 days from api.alternative.me",
    sections: [
      {
        stats: [
          { label: "Now", value: latest.value + " · " + latest.value_classification },
          { label: "Yesterday", value: data[1].value },
          { label: "Month ago", value: data[data.length - 1].value },
        ],
      },
      { title: "Index", chart: { kind: "line", points } },
      {
        text: "Only api.alternative.me is in this plugin's network allowlist; every origin it reaches is logged in the manager.",
      },
    ],
  };
}
