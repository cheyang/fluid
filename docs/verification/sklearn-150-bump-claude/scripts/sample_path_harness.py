#!/usr/bin/env python3
"""Contract test: the samples/vineyard pipeline still works after the
scikit-learn 1.4.0 -> 1.5.0 bump (PR #6260).

Replicates, without vineyard/k8s, the exact sklearn/joblib/pandas call chain of
samples/vineyard/{preprocess-data,train-data,test-data}:

  preprocess: pandas DataFrame ops + train_test_split(X, y, test_size=0.2, random_state=42)
  train:      LinearRegression().fit(df, df) + joblib.dump(model)
  test:       joblib.load + model.predict + mean_squared_error

PASS = the sample pipeline's contract holds on the installed scikit-learn.
FAIL = the bump broke the sample (import error, API change, or crash).
"""
import sys
import tempfile
import os

import joblib
import pandas as pd
import numpy as np


def main() -> int:
    import sklearn
    from sklearn.linear_model import LinearRegression
    from sklearn.metrics import mean_squared_error
    from sklearn.model_selection import train_test_split

    print(f"scikit-learn {sklearn.__version__} | numpy {np.__version__} | "
          f"pandas {pd.__version__} | joblib {joblib.__version__}")

    rng = np.random.default_rng(42)
    n = 500
    # Mirror preprocess-data.py: a DataFrame with a target column, split 80/20.
    df = pd.DataFrame(rng.normal(size=(n, 4)), columns=list("abcd"))
    df["target"] = 3.0 * df["a"] - 1.5 * df["b"] + 0.5 * df["c"] + rng.normal(scale=0.01, size=n)
    X, y = df.drop(columns=["target"]), df["target"]

    X_train, X_test, y_train, y_test = train_test_split(X, y, test_size=0.2, random_state=42)
    assert len(X_train) == 400 and len(X_test) == 100, "split sizes wrong"

    # Mirror train-data.py: fit on DataFrames, joblib.dump.
    model = LinearRegression()
    model.fit(X_train, y_train)
    with tempfile.TemporaryDirectory() as td:
        model_path = os.path.join(td, "model.pkl")
        joblib.dump(model, model_path)

        # Mirror test-data.py: joblib.load, predict, MSE.
        loaded = joblib.load(model_path)
        y_pred = loaded.predict(X_test)
        err = mean_squared_error(y_test, y_pred)

    assert np.isfinite(err), "MSE not finite"
    assert err < 1e-3, f"MSE too large for a linear ground truth: {err}"

    # Deterministic-split contract: same random_state -> same test index set.
    X_tr2, X_te2, _, _ = train_test_split(X, y, test_size=0.2, random_state=42)
    assert (X_te2.index == X_test.index).all(), "train_test_split not deterministic"

    # Report the numbers so base-vs-head runs can be diffed.
    import hashlib
    print(f"MSE={err!r}")
    print("TEST_INDEX_MD5=%s" % hashlib.md5(
        np.ascontiguousarray(X_test.index.values).tobytes()).hexdigest())
    print("SAMPLE_PATH_CONTRACT: PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
