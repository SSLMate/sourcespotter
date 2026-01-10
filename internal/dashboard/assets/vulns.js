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
				h2.textContent = `⚠ Found ${findingsByOSV.size} vulnerabilit${findingsByOSV.size === 1 ? 'y' : 'ies'}`;
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

					// Show call trace (only the first one, formatted like govulncheck text mode)
					if (vulnFindings.length > 0 && vulnFindings[0].trace && vulnFindings[0].trace.length > 0) {
						const trace = vulnFindings[0].trace;
						
						// Find frames with functions (the actual call chain)
						const funcFrames = trace.filter(f => f.function);
						if (funcFrames.length > 0) {
							const tracesHeader = document.createElement('strong');
							tracesHeader.textContent = 'Example call stack:';
							tracesHeader.className = 'vuln-traces-header';
							content.appendChild(tracesHeader);

							const tracesList = document.createElement('div');
							tracesList.className = 'vuln-traces';

							// Format: file:line:col: caller calls callee
							// Trace is ordered from vulnerable func to user code, so reverse for display
							const reversed = [...funcFrames].reverse();
							for (let i = 0; i < reversed.length; i++) {
								const frame = reversed[i];
								const nextFrame = reversed[i + 1]; // the function being called
								
								const line = document.createElement('div');
								line.className = 'vuln-trace-line';
								
								// Line number prefix
								const numSpan = document.createElement('span');
								numSpan.className = 'vuln-trace-num';
								numSpan.textContent = `#${i + 1}: `;
								line.appendChild(numSpan);
								
								// File location with link
								if (frame.position && frame.position.filename) {
									const pos = frame.position;
									const locText = `${pos.filename}:${pos.line || 0}:${pos.column || 0}`;
									
									if (frame.module && frame.version) {
										const link = document.createElement('a');
										let url = `https://go-mod-viewer.appspot.com/${frame.module}@${frame.version}/${pos.filename}`;
										if (pos.line) url += `#L${pos.line}`;
										link.href = url;
										link.textContent = locText;
										link.className = 'vuln-trace-link';
										line.appendChild(link);
									} else {
										line.appendChild(document.createTextNode(locText));
									}
									line.appendChild(document.createTextNode(': '));
								}
								
								// Get short function names (just the type.Method or funcName part)
								const getShortName = (f) => {
									if (!f) return '';
									const pkg = f.package || '';
									const parts = pkg.split('/');
									const shortPkg = parts[parts.length - 1] || '';
									if (f.receiver) {
										return `${shortPkg}.${f.receiver}.${f.function}`;
									}
									return `${shortPkg}.${f.function}`;
								};
								
								const callerName = getShortName(frame);
								const calleeName = nextFrame ? getShortName(nextFrame) : '';
								
								const callSpan = document.createElement('span');
								callSpan.className = 'vuln-trace-call';
								if (calleeName) {
									callSpan.textContent = `${callerName} calls ${calleeName}`;
								} else {
									// Last frame (the vulnerable function)
									callSpan.textContent = callerName;
								}
								line.appendChild(callSpan);
								
								tracesList.appendChild(line);
							}
							
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
