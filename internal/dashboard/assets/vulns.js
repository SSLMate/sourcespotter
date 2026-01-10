(function() {
	let inflightRequest = null;
	const intro = document.getElementById('vulns-intro');
	const form = document.getElementById('vulns-form');
	const errors = document.getElementById('vulns-errors');
	const results = document.getElementById('vulns-results');
	const progress = document.getElementById('vulns-progress');
	const configDetails = document.getElementById('vulns-config');

	function getFormData() {
		let formData = new URLSearchParams(new FormData(form));
		if (formData.get('target') === 'linux/amd64') { formData.delete('target'); }
		if (formData.get('tags') === '') { formData.delete('tags'); }
		return formData;
	}
	function setFormData(formData) {
		form['package'].value = formData.get('package') ?? '';
		form['target'].value = formData.get('target') ?? 'linux/amd64';
		form['tags'].value = formData.get('tags') ?? '';
		form['test'].checked = formData.get('test') === '1';

		configDetails.open =
			form['target'].value !== 'linux/amd64' ||
			form['tags'].value !== '' ||
			form['test'].checked;
	}
	function loadState() {
		const formData = new URL(location).searchParams;
		setFormData(formData);
		loadResults(formData);
	}
	form.onsubmit = () => {
		const formData = getFormData();
		const url = new URL(location);
		url.search = '?' + formData.toString();
		history.pushState({}, "", url);
		loadResults(formData);
		return false;
	};
	window.addEventListener('popstate', loadState);
	loadState();

	async function loadResults(formData) {
		if (inflightRequest) {
			inflightRequest.abort();
			inflightRequest = null;
		}

		const pkg = formData.get('package') ?? '';
		if (pkg === '') {
			intro.hidden = false;
			errors.hidden = true;
			results.hidden = true;
			progress.hidden = true;
			return;
		}

		intro.hidden = true;
		errors.hidden = true;
		results.hidden = true;
		progress.hidden = false;

		const url = form.action + '?' + formData.toString();
		try {
			inflightRequest = new AbortController();
			const resp = await fetch(url, {signal: inflightRequest.signal});
			if (!resp.ok) {
				const body = await resp.text().catch(() => '');
				const msg = body || `Request to ${url} failed with status ${resp.status} ${resp.statusText}`;
				errors.textContent = msg;
				errors.hidden = false;
				results.hidden = true;
				intro.hidden = true;
				progress.hidden = true;
				return;
			}

			// Parse JSONL response
			const text = await resp.text();
			const lines = text.split(/\r?\n/).filter(line => line.trim());

			// Parse all JSON objects from the stream
			const osvEntries = new Map(); // OSV ID -> OSV entry
			const findings = [];          // Vulnerability findings
			let config = null;

			for (const line of lines) {
				try {
					const obj = JSON.parse(line);
					if (obj.config) {
						config = obj.config;
					} else if (obj.osv) {
						osvEntries.set(obj.osv.id, obj.osv);
					} else if (obj.finding) {
						findings.push(obj.finding);
					}
					// Ignore progress messages
				} catch (e) {
					// Skip malformed lines
				}
			}

			// Filter to only reachable findings (those with a function in the trace)
			// Module-level and package-level findings don't have function info
			const reachableFindings = findings.filter(finding => {
				if (!finding.trace || finding.trace.length === 0) return false;
				return finding.trace.some(frame => frame.function);
			});

			// Group findings by OSV ID
			const findingsByOSV = new Map();
			for (const finding of reachableFindings) {
				const id = finding.osv;
				if (!findingsByOSV.has(id)) {
					findingsByOSV.set(id, []);
				}
				findingsByOSV.get(id).push(finding);
			}

			// Build results DOM
			results.innerHTML = '';

			if (findingsByOSV.size === 0) {
				const h2 = document.createElement('h2');
				h2.className = 'vulns-safe';
				h2.textContent = '✓ No vulnerabilities found';
				results.appendChild(h2);

				const p = document.createElement('p');
				p.textContent = 'govulncheck did not find any known vulnerabilities affecting this package.';
				results.appendChild(p);
			} else {
				const h2 = document.createElement('h2');
				h2.className = 'vulns-warning';
				h2.textContent = `⚠ Found ${findingsByOSV.size} vulnerability${findingsByOSV.size === 1 ? '' : 'ies'}`;
				results.appendChild(h2);

				const vulnList = document.createElement('div');
				vulnList.className = 'vulns-list';

				// Sort by OSV ID
				const sortedOSVs = [...findingsByOSV.keys()].sort();

				for (const osvId of sortedOSVs) {
					const osv = osvEntries.get(osvId);
					const vulnFindings = findingsByOSV.get(osvId);

					const vulnCard = document.createElement('details');
					vulnCard.className = 'vuln-card';
					vulnCard.open = true;

					const summary = document.createElement('summary');
					summary.className = 'vuln-summary';

					const idLink = document.createElement('a');
					idLink.href = `https://pkg.go.dev/vuln/${osvId}`;
					idLink.textContent = osvId;
					idLink.className = 'vuln-id';
					idLink.addEventListener('click', e => e.stopPropagation());

					const title = document.createElement('span');
					title.className = 'vuln-title';
					title.textContent = osv ? osv.summary : 'Unknown vulnerability';

					summary.appendChild(idLink);
					summary.appendChild(document.createTextNode(' – '));
					summary.appendChild(title);
					vulnCard.appendChild(summary);

					const content = document.createElement('div');
					content.className = 'vuln-content';

					// Show description if available
					if (osv && osv.details) {
						const desc = document.createElement('p');
						desc.className = 'vuln-description';
						desc.textContent = osv.details;
						content.appendChild(desc);
					}

					// Show affected modules
					if (osv && osv.affected) {
						for (const affected of osv.affected) {
							const affectedDiv = document.createElement('div');
							affectedDiv.className = 'vuln-affected';

							const moduleLabel = document.createElement('strong');
							moduleLabel.textContent = 'Module: ';
							affectedDiv.appendChild(moduleLabel);

							const moduleCode = document.createElement('code');
							moduleCode.textContent = affected.package?.name || affected.module?.path || 'unknown';
							affectedDiv.appendChild(moduleCode);

							// Show version range
							if (affected.ranges) {
								for (const range of affected.ranges) {
									if (range.events) {
										const versionInfo = document.createElement('div');
										versionInfo.className = 'vuln-versions';
										let introduced = null;
										let fixed = null;
										for (const event of range.events) {
											if (event.introduced !== undefined) introduced = event.introduced || '0';
											if (event.fixed !== undefined) fixed = event.fixed;
										}
										if (introduced !== null || fixed !== null) {
											let text = 'Affected: ';
											if (introduced && fixed) {
												text += `v${introduced} to v${fixed} (exclusive)`;
											} else if (introduced) {
												text += `>= v${introduced}`;
											} else if (fixed) {
												text += `< v${fixed}`;
											}
											if (fixed) {
												text += ` — Fixed in v${fixed}`;
											}
											versionInfo.textContent = text;
											affectedDiv.appendChild(versionInfo);
										}
									}
								}
							}

							content.appendChild(affectedDiv);
						}
					}

					// Show call traces
					if (vulnFindings.length > 0) {
						const tracesHeader = document.createElement('strong');
						tracesHeader.textContent = 'Call stacks:';
						tracesHeader.className = 'vuln-traces-header';
						content.appendChild(tracesHeader);

						const tracesList = document.createElement('ul');
						tracesList.className = 'vuln-traces';

						for (const finding of vulnFindings) {
							if (finding.trace && finding.trace.length > 0) {
								const traceLi = document.createElement('li');
								const traceCode = document.createElement('code');
								traceCode.className = 'vuln-trace';

								// Build call chain from trace (reversed, as trace goes from vuln to caller)
								const frames = [...finding.trace].reverse();
								let first = true;
								for (const frame of frames) {
									let label = '';
									if (frame.function) {
										label = `${frame.package || ''}.${frame.function}`;
									} else if (frame.package) {
										label = frame.package;
									} else if (frame.module) {
										label = frame.module;
									}
									if (!label) continue;

									if (!first) {
										traceCode.appendChild(document.createTextNode(' → '));
									}
									first = false;

									// Create link if we have position info
									if (frame.position && frame.position.filename && frame.module && frame.version) {
										const link = document.createElement('a');
										let url = `https://go-mod-viewer.appspot.com/${frame.module}@${frame.version}/${frame.position.filename}`;
										if (frame.position.line) {
											url += `#L${frame.position.line}`;
										}
										link.href = url;
										link.textContent = label;
										link.className = 'vuln-trace-link';
										traceCode.appendChild(link);
									} else {
										traceCode.appendChild(document.createTextNode(label));
									}
								}

								traceLi.appendChild(traceCode);
								tracesList.appendChild(traceLi);
							}
						}

						if (tracesList.children.length > 0) {
							content.appendChild(tracesList);
						}
					}

					// Show references
					if (osv && osv.references && osv.references.length > 0) {
						const refsDiv = document.createElement('div');
						refsDiv.className = 'vuln-refs';

						const refsLabel = document.createElement('strong');
						refsLabel.textContent = 'References: ';
						refsDiv.appendChild(refsLabel);

						const refsList = document.createElement('span');
						let first = true;
						for (const ref of osv.references) {
							if (ref.url) {
								if (!first) refsList.appendChild(document.createTextNode(', '));
								const link = document.createElement('a');
								link.href = ref.url;
								link.textContent = ref.type || 'link';
								refsList.appendChild(link);
								first = false;
							}
						}
						refsDiv.appendChild(refsList);
						content.appendChild(refsDiv);
					}

					vulnCard.appendChild(content);
					vulnList.appendChild(vulnCard);
				}

				results.appendChild(vulnList);
			}

			errors.hidden = true;
			results.hidden = false;
			intro.hidden = true;
			progress.hidden = true;
		} catch (err) {
			if (!(err instanceof DOMException && err.name === "AbortError")) {
				errors.textContent = url + ": " + String(err);
				errors.hidden = false;
				results.hidden = true;
				intro.hidden = true;
				progress.hidden = true;
			}
		}
	}
})();
